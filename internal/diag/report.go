package diag

import (
	"archive/zip"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// File is one entry of the report.
type File struct {
	Name string
	Data []byte
}

// Source produces report files. A failing source adds a note instead of
// aborting the report: a broken VM is exactly when a report is needed.
type Source struct {
	Name    string
	Collect func(ctx context.Context) ([]File, error)
}

// Write collects all sources, redacts every file and writes a zip at path.
// secrets are values to mask everywhere (for example the proxy URL).
func Write(ctx context.Context, path string, sources []Source, secrets ...string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(f)
	var notes []string
	for _, s := range sources {
		files, err := s.Collect(ctx)
		if err != nil {
			notes = append(notes, fmt.Sprintf("%s: %v", s.Name, err))
		}
		for _, file := range files {
			w, err := zw.CreateHeader(&zip.FileHeader{Name: file.Name, Method: zip.Deflate, Modified: time.Now()})
			if err != nil {
				_ = f.Close()
				return err
			}
			if _, err := w.Write([]byte(Redact(string(file.Data), secrets...))); err != nil {
				_ = f.Close()
				return err
			}
		}
	}
	summary := "診断レポート\n作成: " + time.Now().Format(time.RFC3339) + "\n認証情報、トークン、プロキシの値は xxxxx に置き換えています。\n"
	if len(notes) > 0 {
		summary += "\n取得できなかったもの:\n"
		for _, n := range notes {
			summary += "- " + n + "\n"
		}
	}
	if w, err := zw.Create("README.txt"); err == nil {
		_, _ = w.Write([]byte(Redact(summary, secrets...)))
	}
	if err := zw.Close(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
