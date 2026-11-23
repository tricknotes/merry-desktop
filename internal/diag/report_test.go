package diag

import (
	"archive/zip"
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteRedactsAndKeepsGoing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out", "report.zip")
	sources := []Source{
		{Name: "ok", Collect: func(context.Context) ([]File, error) {
			return []File{{Name: "vm/log.txt", Data: []byte("pull via http://bob:pw123@proxy:3128\nAuthorization: Bearer tok")}}, nil
		}},
		{Name: "broken", Collect: func(context.Context) ([]File, error) { return nil, errors.New("VM is not running") }},
	}
	if err := Write(context.Background(), path, sources, "http://proxy.secret:8080"); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	contents := map[string]string{}
	for _, f := range zr.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		contents[f.Name] = string(b)
	}
	if log := contents["vm/log.txt"]; strings.Contains(log, "pw123") || strings.Contains(log, "tok") {
		t.Errorf("secret left in the report: %q", log)
	}
	if !strings.Contains(contents["README.txt"], "broken: VM is not running") {
		t.Errorf("README lacks the failed source: %q", contents["README.txt"])
	}
}
