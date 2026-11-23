package core

import (
	"archive/zip"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tricknotes/merry-desktop/internal/vm"
)

type diagnosing struct {
	*provisioning
}

func (d *diagnosing) Diagnostics(context.Context, []string) ([]vm.DiagnosticFile, error) {
	return []vm.DiagnosticFile{{Name: "vm/dockerd.log", Data: []byte("pulling via http://u:pw@proxy:8080 ok")}}, nil
}

func TestWriteDiagnostics(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "app.log"), []byte("msg=started token=abc123\n"), 0o644)
	c := NewController(&diagnosing{provisioning: newProvisioning()}, nil)
	out := filepath.Join(dir, "report.zip")
	if err := c.WriteDiagnostics(context.Background(), out, DiagnosticsInput{AppLogDir: dir, SettingsPath: filepath.Join(dir, "none.json")}); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.OpenReader(out)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	all := ""
	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		all += string(b)
	}
	for _, want := range []string{"app/summary.json", "app/app.log", "vm/dockerd.log", "README.txt"} {
		if !names[want] {
			t.Errorf("missing %s (have %v)", want, names)
		}
	}
	for _, secret := range []string{"abc123", "pw@", "u:pw", "proxy:8080"} {
		if strings.Contains(all, secret) {
			t.Errorf("report contains %q", secret)
		}
	}
}
