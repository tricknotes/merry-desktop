package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"

	"github.com/tricknotes/merry-desktop/internal/appinfo"
	"github.com/tricknotes/merry-desktop/internal/diag"
	"github.com/tricknotes/merry-desktop/internal/vm"
)

// DiagnosticsInput is what the app adds to the report besides the backend.
type DiagnosticsInput struct {
	AppLogDir    string   // app.log and app.log.1
	SettingsPath string   // the app settings (registries)
	Registries   []string // also checked for reachability from the VM
}

// WriteDiagnostics writes the diagnostic report (F-14) to path.
func (c *Controller) WriteDiagnostics(ctx context.Context, path string, in DiagnosticsInput) error {
	var secrets []string
	if p, ok := c.backend.(vm.Provisioner); ok {
		if n, err := p.DetectNetwork(ctx); err == nil && n.ProxyURL != "" {
			secrets = append(secrets, n.ProxyURL)
		}
	}
	sources := []diag.Source{
		{Name: "app", Collect: func(context.Context) ([]diag.File, error) {
			summary, _ := json.MarshalIndent(map[string]any{
				"app": appinfo.DisplayName, "os": runtime.GOOS, "arch": runtime.GOARCH,
				"snapshot": c.Snapshot(),
			}, "", "  ")
			files := []diag.File{{Name: "app/summary.json", Data: summary}}
			for _, name := range []string{"app.log.1", "app.log"} {
				if data, err := os.ReadFile(filepath.Join(in.AppLogDir, name)); err == nil {
					files = append(files, diag.File{Name: "app/" + name, Data: data})
				}
			}
			if data, err := os.ReadFile(in.SettingsPath); err == nil {
				files = append(files, diag.File{Name: "app/settings.json", Data: data})
			}
			return files, nil
		}},
	}
	if d, ok := c.backend.(vm.Diagnoser); ok {
		sources = append(sources, diag.Source{Name: "vm", Collect: func(ctx context.Context) ([]diag.File, error) {
			files, err := d.Diagnostics(ctx, in.Registries)
			out := make([]diag.File, len(files))
			for i, f := range files {
				out[i] = diag.File{Name: f.Name, Data: f.Data}
			}
			return out, err
		}})
	}
	return diag.Write(ctx, path, sources, secrets...)
}
