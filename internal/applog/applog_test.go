package applog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenRotates(t *testing.T) {
	dir := t.TempDir()
	big := make([]byte, MaxSize+1)
	_ = os.WriteFile(filepath.Join(dir, "app.log"), big, 0o644)
	log, closer, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	log.Info("hello")
	closer.Close()
	if fi, err := os.Stat(filepath.Join(dir, "app.log.1")); err != nil || fi.Size() != MaxSize+1 {
		t.Fatalf("rotated file: %v, %v", fi, err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "app.log"))
	if !strings.Contains(string(data), "msg=hello") {
		t.Fatalf("new log = %q", data)
	}
}
