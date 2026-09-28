package logfile

import (
	"errors"
	"os"
	"path/filepath"
	"sync"

	"diskord/internal/securefs"
)

const MaxBytes int64 = 10 << 20
const Backups = 3

type Writer struct {
	mu         sync.Mutex
	root, name string
	file       *os.File
	size       int64
}

func path(root, name string) (string, error) {
	if name != "diskord" && name != "discord" {
		return "", errors.New("invalid log name")
	}
	if _, err := securefs.Dir(root, "logs"); err != nil {
		return "", err
	}
	return securefs.Within(root, filepath.Join("logs", name+".log"))
}

func Open(root, name string) (*Writer, error) {
	p, err := path(root, name)
	if err != nil {
		return nil, err
	}
	w := &Writer{root: root, name: name}
	if err = w.open(p); err != nil {
		return nil, err
	}
	if w.size >= MaxBytes {
		if err = w.rotate(); err != nil {
			w.file.Close()
			return nil, err
		}
	}
	return w, nil
}
func (w *Writer) open(p string) error {
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	if err = securefs.Private(p); err != nil {
		f.Close()
		return err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	w.file, w.size = f, info.Size()
	return nil
}
func (w *Writer) rotate() error {
	if err := w.file.Close(); err != nil {
		return err
	}
	p, err := path(w.root, w.name)
	if err != nil {
		return err
	}
	for i := Backups; i > 0; i-- {
		old := p
		if i > 1 {
			old += "." + string(rune('0'+i-1))
		}
		newPath := p + "." + string(rune('0'+i))
		if _, err = securefs.Within(w.root, filepath.Join("logs", filepath.Base(old))); err != nil {
			return err
		}
		if _, err = securefs.Within(w.root, filepath.Join("logs", filepath.Base(newPath))); err != nil {
			return err
		}
		if err = os.Remove(newPath); err != nil && !os.IsNotExist(err) {
			return err
		}
		if err = os.Rename(old, newPath); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return w.open(p)
}
func (w *Writer) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return 0, os.ErrClosed
	}
	if w.size > 0 && w.size+int64(len(data)) > MaxBytes {
		if err := w.rotate(); err != nil {
			w.file = nil
			return 0, err
		}
	}
	n, err := w.file.Write(data)
	w.size += int64(n)
	return n, err
}
func (w *Writer) File() *os.File { w.mu.Lock(); defer w.mu.Unlock(); return w.file }
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Sync()
	err = errors.Join(err, w.file.Close())
	w.file = nil
	return err
}
