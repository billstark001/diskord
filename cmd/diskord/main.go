package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"diskord/internal/app"
	"diskord/internal/ca"
	"diskord/internal/config"
	"diskord/internal/securefs"
	"diskord/internal/ui"
)

const help = `diskord v0.1 — local, explicitly configured Discord traffic observer

Usage:
  diskord [--config diskord.yaml] init [--runtime-dir PATH] [--openssl PATH]
  diskord [--config diskord.yaml] ca issue --cert ca/root.pem --key ca/root.key
  diskord [--config diskord.yaml] ca select --cert ca/root.pem --key ca/root.key
  diskord [--config diskord.yaml] config set resources.enabled true|false
  diskord [--config diskord.yaml] config set logging.file.enabled true|false
  diskord [--config diskord.yaml] config set logging.discord.enabled true|false
  diskord [--config diskord.yaml] discord launch [--path EXECUTABLE]
  diskord [--config diskord.yaml] doctor
  diskord [--config diskord.yaml] ui-token
  diskord [--config diskord.yaml] run

With no command, run is assumed. The default runtime directory is cwd.
The proxy does not modify system proxy settings or install CA trust automatically.
Only operate on your own device/account or traffic you are authorized to inspect.
`

func main() {
	if e := run(os.Args[1:]); e != nil {
		fmt.Fprintln(os.Stderr, "diskord:", e)
		os.Exit(1)
	}
}
func arguments(args []string) (string, []string, error) {
	path := "diskord.yaml"
	rest := []string{}
	for i := 0; i < len(args); i++ {
		if args[i] == "--config" {
			if i+1 == len(args) {
				return "", nil, errors.New("--config needs a path")
			}
			i++
			path = args[i]
		} else if strings.HasPrefix(args[i], "--config=") {
			path = strings.TrimPrefix(args[i], "--config=")
		} else {
			rest = append(rest, args[i])
		}
	}
	if path == "" {
		return "", nil, errors.New("empty config path")
	}
	return path, rest, nil
}
func run(args []string) error {
	path, args, e := arguments(args)
	if e != nil {
		return e
	}
	if len(args) == 0 {
		args = []string{"run"}
	}
	switch args[0] {
	case "help", "-h", "--help":
		fmt.Print(help)
		return nil
	case "version", "--version":
		fmt.Println("diskord v0.1.0-prototype")
		return nil
	case "init":
		return initialize(path, args[1:])
	case "run":
		if len(args) != 1 {
			return errors.New("unexpected run arguments")
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return app.Run(ctx, path)
	}
	m, e := config.New(path)
	if e != nil {
		return e
	}
	if args[0] == "ui-token" {
		if len(args) != 1 {
			return errors.New("unexpected ui-token arguments")
		}
		token, e := ui.Token(m.Root)
		if e != nil {
			return e
		}
		fmt.Println(token)
		return nil
	}
	if args[0] == "doctor" {
		return doctor(m)
	}
	if args[0] == "discord" {
		if len(args) < 2 || args[1] != "launch" {
			return errors.New("use discord launch [--path EXECUTABLE]")
		}
		return launchDiscord(m, args[2:])
	}
	lock, e := securefs.Acquire(m.Root)
	if e != nil {
		return fmt.Errorf("%w; while running, use the authenticated web controls", e)
	}
	defer lock.Close()
	switch args[0] {
	case "ca":
		if len(args) < 2 {
			return errors.New("use ca issue or ca select")
		}
		fs := flag.NewFlagSet("ca", flag.ContinueOnError)
		certPath := fs.String("cert", "", "explicit certificate path under runtime")
		keyPath := fs.String("key", "", "explicit key path under runtime")
		tool := fs.String("openssl", m.Current().CA.OpenSSL, "system OpenSSL executable")
		if e = fs.Parse(args[2:]); e != nil {
			return e
		}
		if fs.NArg() != 0 || *certPath == "" || *keyPath == "" {
			return errors.New("explicit --cert and --key are required")
		}
		switch args[1] {
		case "issue":
			if e = ca.Issue(context.Background(), m.Root, *certPath, *keyPath, *tool); e != nil {
				return e
			}
			a, e := ca.Load(m.Root, *certPath, *keyPath)
			if e != nil {
				return e
			}
			fmt.Printf("CA issued, not selected or trusted.\nCertificate: %s\nKey: %s\nSHA-256: %s\n", *certPath, *keyPath, a.Fingerprint())
			return nil
		case "select":
			var selected *ca.Authority
			_, e = m.Patch(m.Revision(), map[string]any{"ca.cert": *certPath, "ca.key": *keyPath}, func(c config.Config) error { var e error; selected, e = ca.Load(m.Root, c.CA.Cert, c.CA.Key); return e })
			if e != nil {
				return e
			}
			fmt.Printf("CA selected. SHA-256: %s\nInstall only the public certificate manually, never the private key.\n", selected.Fingerprint())
			if !selected.TrustedForTLS() {
				fmt.Println(ca.TrustInstructions())
			}
			return nil
		default:
			return errors.New("unknown CA operation")
		}
	case "config":
		if len(args) != 4 || args[1] != "set" || (args[2] != "resources.enabled" && args[2] != "logging.file.enabled" && args[2] != "logging.discord.enabled") {
			return errors.New("supported settings: resources.enabled, logging.file.enabled, logging.discord.enabled; select certificates using ca select")
		}
		enabled, e := strconv.ParseBool(args[3])
		if e != nil {
			return e
		}
		_, e = m.Patch(m.Revision(), map[string]any{args[2]: enabled}, nil)
		if e != nil {
			return e
		}
		fmt.Println("Configuration updated.")
		return nil
	default:
		return errors.New("unknown command; run diskord --help")
	}
}
func initialize(path string, args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	runtime := fs.String("runtime-dir", ".", "runtime directory; defaults to cwd")
	tool := fs.String("openssl", "openssl", "system OpenSSL executable")
	if e := fs.Parse(args); e != nil {
		return e
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected init arguments")
	}
	root, e := securefs.Root(*runtime)
	if e != nil {
		return e
	}
	if e = securefs.Prepare(root); e != nil {
		return e
	}
	lock, e := securefs.Acquire(root)
	if e != nil {
		return e
	}
	defer lock.Close()
	target, e := filepath.Abs(path)
	if e != nil {
		return e
	}
	f, e := securefs.NewFile(target)
	if e != nil {
		return fmt.Errorf("refusing to overwrite config or create it in a missing parent: %w", e)
	}
	f.Close()
	success := false
	defer func() {
		if !success {
			os.Remove(target)
		}
	}()
	storedRoot := root
	if *runtime == "." {
		storedRoot = "."
	}
	text := strings.Replace(config.Example, "runtime_dir: .", "runtime_dir: "+strconv.Quote(storedRoot), 1)
	text = strings.Replace(text, "openssl: openssl", "openssl: "+strconv.Quote(*tool), 1)
	if e = securefs.AtomicWrite(root, target, []byte(text)); e != nil {
		return e
	}
	if _, e = ui.Token(root); e != nil {
		return e
	}
	success = true
	fmt.Printf("Initialized.\nConfig: %s\nRuntime: %s\nNext: diskord ca issue --cert ca/root.pem --key ca/root.key\n", target, root)
	return nil
}
func doctor(m *config.Manager) error {
	c := m.Current()
	fmt.Printf("Config: valid\nRuntime: %s\nProxy: %s\nWeb: %s\n", m.Root, c.Proxy.Listen, c.Web.Listen)
	if p, e := exec.LookPath(c.CA.OpenSSL); e == nil {
		fmt.Println("CA toolchain:", p)
	} else {
		fmt.Println("CA toolchain: MISSING (issuance unavailable; existing CA can still run)")
	}
	if c.CA.Cert == "" {
		return errors.New("no CA selected")
	}
	a, e := ca.Load(m.Root, c.CA.Cert, c.CA.Key)
	if e != nil {
		return e
	}
	fmt.Printf("CA: valid\nSHA-256: %s\nExpires: %s\n", a.Fingerprint(), a.Root.NotAfter.Format(time.RFC3339))
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil}}
	response, e := client.Get("http://" + c.Web.Listen + "/healthz")
	if e != nil {
		fmt.Println("Control plane: not reachable (normal before run)")
	} else {
		response.Body.Close()
		fmt.Println("Control plane HTTP status:", response.StatusCode)
	}
	fmt.Println("Doctor does not prove that Chrome/Discord routes through the proxy or trusts the CA.")
	return nil
}
