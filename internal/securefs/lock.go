package securefs

import (
	"errors"
	"os"
)

type Lock struct{ f *os.File }

func Acquire(root string) (*Lock, error) {
	p, e := Within(root, "state/diskord.lock")
	if e != nil {
		return nil, e
	}
	f, e := os.OpenFile(p, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	if e = Private(p); e != nil {
		f.Close()
		return nil, e
	}
	if e = lockFile(f); e != nil {
		f.Close()
		return nil, errors.New("runtime directory is already in use by another diskord process")
	}
	return &Lock{f}, nil
}
func (l *Lock) Close() error {
	if l == nil || l.f == nil {
		return nil
	}
	_ = unlockFile(l.f)
	return l.f.Close()
}
