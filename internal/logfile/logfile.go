package logfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"diskord/internal/securefs"
)

const MaxBytes int64 = 10 << 20
const Backups = 3

type Writer struct {
	mu   sync.Mutex
	root string
	path string
	file *os.File
	size int64
}

// OpenOutputAt creates a private log file for one invocation.
func OpenOutputAt(root, name string, started time.Time) (*os.File, string, error) {
	if name != "diskord" && name != "discord" && name != "diskord-bg" {
		return nil, "", errors.New("invalid log name")
	}
	if _, err := securefs.Dir(root, "logs"); err != nil {
		return nil, "", err
	}
	stamp := started.UTC().Format("20060102T150405.000000000Z")
	for attempt := 0; attempt < 100; attempt++ {
		filename := fmt.Sprintf("%s-%s.log", name, stamp)
		if attempt > 0 {
			filename = fmt.Sprintf("%s-%s-%d.log", name, stamp, attempt)
		}
		path, err := securefs.Within(root, filepath.Join("logs", filename))
		if err != nil {
			return nil, "", err
		}
		file, err := securefs.NewFile(path)
		if err == nil {
			return file, path, nil
		}
		if !os.IsExist(err) {
			return nil, "", err
		}
	}
	return nil, "", errors.New("too many logs with the same start timestamp")
}

func Open(root, name string) (*Writer, error) {
	return OpenAt(root, name, time.Now())
}

func OpenAt(root, name string, started time.Time) (*Writer, error) {
	file, path, err := OpenOutputAt(root, name, started)
	if err != nil {
		return nil, err
	}
	return &Writer{root: root, path: path, file: file}, nil
}

func (w *Writer) Path() string { return w.path }

func (w *Writer) rotate() error {
	if err := w.file.Close(); err != nil {
		return err
	}
	var err error
	for i := Backups; i > 0; i-- {
		old := w.path
		if i > 1 {
			old += fmt.Sprintf(".%d", i-1)
		}
		newPath := fmt.Sprintf("%s.%d", w.path, i)
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
	file, err := securefs.NewFile(w.path)
	if err != nil {
		return err
	}
	w.file, w.size = file, 0
	return nil
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
