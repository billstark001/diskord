package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPatchPreservesCommentsAndCAS(t *testing.T) {
	root := t.TempDir()
	for _, k := range []string{"TMPDIR", "TMP", "TEMP", "SQLITE_TMPDIR", "RANDFILE", "SSLKEYLOGFILE", "SSLKEYLOG_FILE"} {
		t.Setenv(k, os.Getenv(k))
	}
	text := strings.Replace(Example, "runtime_dir: .", "runtime_dir: "+filepath.ToSlash(root), 1) + "\n# user-owned extension\nextensions:\n  note: keep-me\n"
	path := filepath.Join(root, "diskord.yaml")
	if e := os.WriteFile(path, []byte(text), 0600); e != nil {
		t.Fatal(e)
	}
	m, e := New(path)
	if e != nil {
		t.Fatal(e)
	}
	rev := m.Revision()
	if _, e = m.Patch(rev, map[string]any{"resources.enabled": true}, nil); e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	for _, expected := range []string{"keep-me", "# user-owned extension", "enabled: true"} {
		if !strings.Contains(string(b), expected) {
			t.Fatal(expected, string(b))
		}
	}
	if _, e = m.Patch(rev, map[string]any{"resources.enabled": false}, nil); e == nil {
		t.Fatal("stale revision overwrote config")
	}
	c, _, e := Load(path)
	if e != nil || !c.Resources.Enabled {
		t.Fatal(c, e)
	}
}
func TestUnsafeConfigRejected(t *testing.T) {
	for _, replacement := range []string{strings.Replace(Example, "127.0.0.1:3900", "0.0.0.0:3900", 1), strings.Replace(Example, "raw: false", "raw: true", 1), Example + "\n---\nversion: 1\n"} {
		if _, _, e := decode([]byte(replacement)); e == nil {
			t.Fatal("unsafe config accepted")
		}
	}
}
