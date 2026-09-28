package logfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	base := writer.Path()
	if !strings.HasPrefix(filepath.Base(base), "diskord-") || !strings.HasSuffix(base, "Z.log") {
		t.Fatalf("log filename lacks UTC start timestamp: %s", base)
	}
	for _, suffix := range []string{"", ".1", ".2", ".3"} {
		name := base + suffix
		info, err := os.Stat(name)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0077 != 0 {
			t.Fatalf("%s is not private: %s", name, info.Mode())
		}
	}
	if _, err := os.Stat(base + ".4"); !os.IsNotExist(err) {
		t.Fatalf("unexpected extra backup: %v", err)
	}
}

func TestSeparateFilesForSameStartTime(t *testing.T) {
	root := t.TempDir()
	started := time.Date(2026, 9, 29, 1, 2, 3, 4, time.FixedZone("JST", 9*3600))
	first, firstPath, err := OpenOutputAt(root, "discord", started)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, secondPath, err := OpenOutputAt(root, "discord", started)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if firstPath == secondPath || !strings.Contains(filepath.Base(firstPath), "20260928T160203.000000004Z") {
		t.Fatalf("unexpected session paths: %s, %s", firstPath, secondPath)
	}
}

func TestRejectsOtherLogNames(t *testing.T) {
	if _, err := Open(t.TempDir(), "../other"); err == nil {
		t.Fatal("unexpected log name accepted")
	}
}
