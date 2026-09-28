package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"diskord/internal/securefs"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Version    int    `yaml:"version"`
	RuntimeDir string `yaml:"runtime_dir"`
	Web        struct {
		Listen string `yaml:"listen"`
	} `yaml:"web"`
	Proxy struct {
		Listen         string `yaml:"listen"`
		MaxConnections int    `yaml:"max_connections"`
	} `yaml:"proxy"`
	CA struct {
		Cert    string `yaml:"cert"`
		Key     string `yaml:"key"`
		OpenSSL string `yaml:"openssl"`
	} `yaml:"ca"`
	Capture struct {
		MaxHTTPBytes  int  `yaml:"max_http_bytes"`
		MaxEventBytes int  `yaml:"max_event_bytes"`
		Queue         int  `yaml:"queue"`
		Raw           bool `yaml:"raw"`
	} `yaml:"capture"`
	Resources struct {
		Enabled       bool  `yaml:"enabled"`
		MaxBytes      int   `yaml:"max_bytes"`
		MaxTotalBytes int64 `yaml:"max_total_bytes"`
	} `yaml:"resources"`
}

const Example = `# diskord: all relative runtime paths are resolved under runtime_dir.
version: 1
runtime_dir: . # '.' means the process working directory, NOT the YAML directory.
web:
  listen: 127.0.0.1:3900
proxy:
  listen: 127.0.0.1:3901
  max_connections: 128
ca:
  cert: ""
  key: ""
  openssl: openssl # An executable name in PATH, or an explicit absolute path.
capture:
  max_http_bytes: 8388608
  max_event_bytes: 16777216
  queue: 128
  raw: false # v0.1 rejects true: no raw traffic database exists.
resources:
  enabled: false
  max_bytes: 8388608
  max_total_bytes: 536870912
`

func Default() Config { var c Config; _ = yaml.Unmarshal([]byte(Example), &c); return c }
func loopback(addr string) error {
	h, p, e := net.SplitHostPort(addr)
	if e != nil {
		return errors.New("listen must be an IP:port")
	}
	ip := net.ParseIP(h)
	n, e := strconv.Atoi(p)
	if ip == nil || !ip.IsLoopback() || e != nil || n < 1 || n > 65535 {
		return errors.New("only literal loopback IP addresses and ports 1..65535 are allowed")
	}
	return nil
}
func (c Config) Validate() error {
	if c.Version != 1 {
		return errors.New("unsupported config version")
	}
	if e := loopback(c.Web.Listen); e != nil {
		return fmt.Errorf("web.listen: %w", e)
	}
	if e := loopback(c.Proxy.Listen); e != nil {
		return fmt.Errorf("proxy.listen: %w", e)
	}
	if c.Web.Listen == c.Proxy.Listen {
		return errors.New("web and proxy listeners must differ")
	}
	if c.Proxy.MaxConnections < 1 || c.Proxy.MaxConnections > 1024 {
		return errors.New("proxy.max_connections must be 1..1024")
	}
	if c.Capture.Raw {
		return errors.New("raw capture is deliberately unavailable in v0.1")
	}
	if c.Capture.Queue < 1 || c.Capture.Queue > 2048 {
		return errors.New("capture.queue must be 1..2048")
	}
	for _, n := range []int{c.Capture.MaxHTTPBytes, c.Capture.MaxEventBytes, c.Resources.MaxBytes} {
		if n < 1024 || n > 64*1024*1024 {
			return errors.New("capture/resource object limits must be 1 KiB..64 MiB")
		}
	}
	if c.Resources.MaxTotalBytes < int64(c.Resources.MaxBytes) {
		return errors.New("resource total limit is smaller than its per-file limit")
	}
	if (c.CA.Cert == "") != (c.CA.Key == "") {
		return errors.New("ca.cert and ca.key must be selected together")
	}
	return nil
}
func decode(data []byte) (Config, *yaml.Node, error) {
	c := Default()
	var node yaml.Node
	d := yaml.NewDecoder(bytes.NewReader(data))
	if e := d.Decode(&node); e != nil {
		return c, nil, e
	}
	var extra yaml.Node
	if e := d.Decode(&extra); e != io.EOF {
		return c, nil, errors.New("exactly one YAML document is required")
	}
	if len(node.Content) != 1 || node.Content[0].Kind != yaml.MappingNode {
		return c, nil, errors.New("YAML must be a mapping")
	}
	if e := node.Decode(&c); e != nil {
		return c, nil, e
	}
	return c, &node, c.Validate()
}
func Hash(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func Load(path string) (Config, string, error) {
	b, e := os.ReadFile(path)
	if e != nil {
		return Config{}, "", e
	}
	if len(b) > 1024*1024 {
		return Config{}, "", errors.New("config too large")
	}
	c, _, e := decode(b)
	return c, Hash(b), e
}

type Manager struct {
	mu         sync.Mutex
	Path, Root string
	current    Config
}

func New(path string) (*Manager, error) {
	path, e := filepath.Abs(path)
	if e != nil {
		return nil, e
	}
	c, _, e := Load(path)
	if e != nil {
		return nil, e
	}
	root, e := securefs.Root(c.RuntimeDir)
	if e != nil {
		return nil, e
	}
	if e = securefs.Prepare(root); e != nil {
		return nil, e
	}
	if c.CA.Cert != "" {
		if _, e = securefs.Within(root, c.CA.Cert); e != nil {
			return nil, e
		}
		if _, e = securefs.Within(root, c.CA.Key); e != nil {
			return nil, e
		}
	}
	return &Manager{Path: path, Root: root, current: c}, nil
}
func (m *Manager) Current() Config { m.mu.Lock(); defer m.mu.Unlock(); return m.current }
func (m *Manager) Revision() string {
	b, e := os.ReadFile(m.Path)
	if e != nil {
		return ""
	}
	return Hash(b)
}
func patch(node *yaml.Node, path []string, value any) error {
	if node.Kind != yaml.MappingNode {
		return errors.New("cannot patch a non-mapping or YAML alias")
	}
	var child *yaml.Node
	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Value == path[0] {
			child = node.Content[i+1]
			break
		}
	}
	if child == nil {
		child = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: path[0]}, child)
	}
	if len(path) > 1 {
		return patch(child, path[1:], value)
	}
	head, line, foot := child.HeadComment, child.LineComment, child.FootComment
	var replacement yaml.Node
	if e := replacement.Encode(value); e != nil {
		return e
	}
	*child = replacement
	child.HeadComment, child.LineComment, child.FootComment = head, line, foot
	return nil
}
func (m *Manager) Patch(expected string, changes map[string]any, validate func(Config) error) (Config, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	old, e := os.ReadFile(m.Path)
	if e != nil {
		return Config{}, e
	}
	if expected != "" && Hash(old) != expected {
		return Config{}, errors.New("configuration changed; reload before saving")
	}
	_, node, e := decode(old)
	if e != nil {
		return Config{}, e
	}
	// Fixed ordering makes YAML updates deterministic and preserves unrelated nodes/comments.
	for _, k := range []string{"ca.cert", "ca.key", "resources.enabled"} {
		if v, ok := changes[k]; ok {
			if e = patch(node.Content[0], strings.Split(k, "."), v); e != nil {
				return Config{}, e
			}
		}
	}
	for k := range changes {
		if k != "ca.cert" && k != "ca.key" && k != "resources.enabled" {
			return Config{}, fmt.Errorf("field %s requires a manual edit and restart", k)
		}
	}
	var out bytes.Buffer
	enc := yaml.NewEncoder(&out)
	enc.SetIndent(2)
	if e = enc.Encode(node); e != nil {
		return Config{}, e
	}
	if e = enc.Close(); e != nil {
		return Config{}, e
	}
	c, _, e := decode(out.Bytes())
	if e != nil {
		return Config{}, e
	}
	// Do not claim externally edited restart-only settings have been hot-applied.
	baseline := m.current
	baseline.CA.Cert = c.CA.Cert
	baseline.CA.Key = c.CA.Key
	baseline.Resources.Enabled = c.Resources.Enabled
	if baseline != c {
		return Config{}, errors.New("restart-only settings changed on disk; restart diskord before saving")
	}
	if validate != nil {
		if e = validate(c); e != nil {
			return Config{}, e
		}
	}
	if e = securefs.AtomicWrite(m.Root, m.Path, out.Bytes()); e != nil {
		return Config{}, e
	}
	m.current = c
	return c, nil
}
