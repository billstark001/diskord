package logfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriterRotatesAndKeepsPrivateFiles(t *testing.T) {
	root := t.TempDir()
	writer, err := Open(root, "diskord")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < Backups+2; i++ {
		if _, err := writer.Write([]byte(strings.Repeat("x", int(MaxBytes/2)+1))); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"diskord.log", "diskord.log.1", "diskord.log.2", "diskord.log.3"} {
		info, err := os.Stat(filepath.Join(root, "logs", name))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0077 != 0 {
			t.Fatalf("%s is not private: %s", name, info.Mode())
		}
	}
	if _, err := os.Stat(filepath.Join(root, "logs", "diskord.log.4")); !os.IsNotExist(err) {
		t.Fatalf("unexpected extra backup: %v", err)
	}
}

func TestRejectsOtherLogNames(t *testing.T) {
	if _, err := Open(t.TempDir(), "../other"); err == nil {
		t.Fatal("unexpected log name accepted")
	}
}
