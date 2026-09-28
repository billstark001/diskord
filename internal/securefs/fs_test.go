package securefs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPathsAndLock(t *testing.T) {
	r := t.TempDir()
	for _, k := range []string{"TMPDIR", "TMP", "TEMP", "SQLITE_TMPDIR", "RANDFILE", "SSLKEYLOGFILE", "SSLKEYLOG_FILE"} {
		t.Setenv(k, os.Getenv(k))
	}
	if e := Prepare(r); e != nil {
		t.Fatal(e)
	}
	if _, e := Within(r, "../escape"); e == nil {
		t.Fatal("escape")
	}
	if e := os.Symlink(t.TempDir(), filepath.Join(r, "linked")); e == nil {
		if _, e = Within(r, "linked/a"); e == nil {
			t.Fatal("symlink")
		}
	}
	l, e := Acquire(r)
	if e != nil {
		t.Fatal(e)
	}
	if other, e := Acquire(r); e == nil {
		other.Close()
		t.Fatal("double lock")
	}
	l.Close()
	if e := AtomicWrite(r, filepath.Join(r, "state", "a"), []byte("first")); e != nil {
		t.Fatal(e)
	}
	if e := AtomicWrite(r, filepath.Join(r, "state", "a"), []byte("second")); e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(filepath.Join(r, "state", "a"))
	if e != nil || string(b) != "second" {
		t.Fatal(e, string(b))
	}
}
