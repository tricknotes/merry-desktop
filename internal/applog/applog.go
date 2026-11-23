// Package applog keeps the app log in a file (REQUIREMENTS.md 6, observability),
// so that the diagnostic report (F-14) can include it.
package applog

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
)

// MaxSize is the size at which the log is rotated (one old file is kept).
const MaxSize = 5 << 20

// Open opens dir/app.log for appending, rotating it to app.log.1 first when it
// is larger than MaxSize. The returned logger also writes to stderr.
func Open(dir string) (*slog.Logger, io.Closer, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, err
	}
	path := filepath.Join(dir, "app.log")
	if fi, err := os.Stat(path); err == nil && fi.Size() > MaxSize {
		_ = os.Rename(path, path+".1")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, nil, err
	}
	return slog.New(slog.NewTextHandler(io.MultiWriter(os.Stderr, f), nil)), f, nil
}
