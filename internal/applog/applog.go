// Package applog keeps the app log in a file (REQUIREMENTS.md 6, observability),
// so that the diagnostic report (F-14) can include it.
package applog

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

// MaxSize is the size at which the log is rotated (one old file is kept).
const MaxSize = 5 << 20

// Writer is where the log goes: stderr until OpenFile succeeds, then stderr
// and the file. The file is opened only once the app knows it is the only
// instance, so that a second instance never rotates the first one's log.
type Writer struct {
	mu sync.Mutex
	w  io.Writer
}

// New returns a logger that writes to stderr until w.OpenFile is called.
func New() (*slog.Logger, *Writer) {
	w := &Writer{w: os.Stderr}
	return slog.New(slog.NewTextHandler(w, nil)), w
}

func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.w.Write(p)
}

// OpenFile opens dir/app.log for appending, rotating it to app.log.1 first
// when it is larger than MaxSize, and sends the log there too.
func (w *Writer) OpenFile(dir string) (io.Closer, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "app.log")
	if fi, err := os.Stat(path); err == nil && fi.Size() > MaxSize {
		_ = os.Rename(path, path+".1")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	w.mu.Lock()
	w.w = io.MultiWriter(os.Stderr, f)
	w.mu.Unlock()
	return f, nil
}
