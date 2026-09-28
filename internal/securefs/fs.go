// Package securefs centralizes every application-owned write.
package securefs

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func Root(path string) (string, error) {
	if path == "" {
		path = "."
	}
	p, e := filepath.Abs(path)
	if e != nil {
		return "", e
	}
	if e = os.MkdirAll(p, 0700); e != nil {
		return "", e
	}
	return filepath.EvalSymlinks(p)
}
func Within(root, relative string) (string, error) {
	if relative == "" {
		return "", errors.New("empty runtime path")
	}
	p := relative
	if !filepath.IsAbs(p) {
		p = filepath.Join(root, p)
	}
	p = filepath.Clean(p)
	rel, e := filepath.Rel(root, p)
	if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", errors.New("path escapes runtime directory")
	}
	cur := root
	for _, part := range strings.Split(rel, string(os.PathSeparator)) {
		if part == "." {
			continue
		}
		cur = filepath.Join(cur, part)
		st, e := os.Lstat(cur)
		if e != nil {
			if os.IsNotExist(e) {
				continue
			}
			return "", e
		}
		if st.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("runtime symlinks/junctions are not allowed")
		}
	}
	return p, nil
}
func Dir(root, relative string) (string, error) {
	p, e := Within(root, relative)
	if e != nil {
		return "", e
	}
	if e = os.MkdirAll(p, 0700); e != nil {
		return "", e
	}
	if p != root {
		if e = Private(p); e != nil {
			return "", e
		}
	}
	return p, nil
}
func Prepare(root string) error {
	for _, p := range []string{"tmp", "ca", "db", "resources", "state"} {
		if _, e := Dir(root, p); e != nil {
			return e
		}
	}
	for _, k := range []string{"TMPDIR", "TMP", "TEMP", "SQLITE_TMPDIR"} {
		if e := os.Setenv(k, filepath.Join(root, "tmp")); e != nil {
			return e
		}
	}
	_ = os.Setenv("RANDFILE", filepath.Join(root, "tmp", "openssl.rnd"))
	// Upstream libraries must not write TLS session keys to caller-supplied paths.
	_ = os.Unsetenv("SSLKEYLOGFILE")
	_ = os.Unsetenv("SSLKEYLOG_FILE")
	return nil
}
func Random(n int) (string, error) {
	b := make([]byte, n)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	return hex.EncodeToString(b), nil
}
func NewFile(path string) (*os.File, error) {
	f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return nil, e
	}
	if e = Private(path); e != nil {
		f.Close()
		os.Remove(path)
		return nil, e
	}
	return f, nil
}
func AtomicWrite(root, target string, data []byte) error {
	dir, e := Dir(root, "tmp")
	if e != nil {
		return e
	}
	nonce, e := Random(12)
	if e != nil {
		return e
	}
	temp := filepath.Join(dir, "atomic-"+nonce)
	f, e := NewFile(temp)
	if e != nil {
		return e
	}
	defer os.Remove(temp)
	if _, e = f.Write(data); e != nil {
		f.Close()
		return e
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	if e = replace(temp, target); e != nil {
		return fmt.Errorf("atomic replace failed (config and runtime must be on the same filesystem): %w", e)
	}
	return syncDir(filepath.Dir(target))
}
