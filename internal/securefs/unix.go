//go:build !windows

package securefs

import (
	"os"
	"syscall"
)

func Private(path string) error {
	st, e := os.Stat(path)
	if e != nil {
		return e
	}
	mode := os.FileMode(0600)
	if st.IsDir() {
		mode = 0700
	}
	return os.Chmod(path, mode)
}
func replace(src, dst string) error { return os.Rename(src, dst) }
func syncDir(path string) error {
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer f.Close()
	return f.Sync()
}
func lockFile(f *os.File) error   { return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) }
func unlockFile(f *os.File) error { return syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }
