//go:build !windows

package logfile

import (
	"os"
	"testing"
)

func assertPrivateLog(t *testing.T, name string, mode os.FileMode) {
	t.Helper()
	if mode.Perm()&0077 != 0 {
		t.Fatalf("%s is not private: %s", name, mode)
	}
}
