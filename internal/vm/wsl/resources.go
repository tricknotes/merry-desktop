package wsl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/tricknotes/merry-desktop/internal/vm"
)

// Resources on WSL2 (F-03, see REQUIREMENTS.md):
//   - CPUs and memory are settings of the whole WSL VM in %UserProfile%\.wslconfig.
//     They affect the user's other distros and need `wsl --shutdown` to apply.
//   - The disk limit belongs to the engine distro only. It is applied with
//     `wsl --manage <distro> --resize` the next time the distro starts.

var _ vm.ResourceManager = (*Backend)(nil)

// state is what the backend remembers between runs.
type state struct {
	// DiskGiB is the requested disk limit; AppliedDiskGiB is the one in effect.
	DiskGiB        int `json:"diskGiB"`
	AppliedDiskGiB int `json:"appliedDiskGiB"`
}

func defaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".wslconfig")
}

func defaultStatePath(distro string) string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, distro, "wsl-state.json")
}

func (b *Backend) loadState() (state, error) {
	var s state
	data, err := os.ReadFile(b.StatePath)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	return s, json.Unmarshal(data, &s)
}

func (b *Backend) saveState(s state) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(b.StatePath, data)
}

func (b *Backend) readWSLConfig() (string, wslConfig, error) {
	data, err := os.ReadFile(b.ConfigPath)
	if errors.Is(err, fs.ErrNotExist) {
		return "", wslConfig{}, nil
	}
	if err != nil {
		return "", wslConfig{}, err
	}
	cfg, err := parseWSLConfig(string(data))
	return string(data), cfg, err
}

func (b *Backend) CurrentResources(context.Context) (vm.Resources, error) {
	_, cfg, err := b.readWSLConfig()
	if err != nil {
		return vm.Resources{}, err
	}
	s, err := b.loadState()
	if err != nil {
		return vm.Resources{}, err
	}
	return vm.Resources{CPUs: cfg.Processors, MemoryMiB: cfg.MemoryMiB, DiskGiB: s.DiskGiB}, nil
}

func (b *Backend) PlanResources(ctx context.Context, r vm.Resources) (vm.ResourcePlan, error) {
	cur, err := b.CurrentResources(ctx)
	if err != nil {
		return vm.ResourcePlan{}, err
	}
	host := r.CPUs != cur.CPUs || r.MemoryMiB != cur.MemoryMiB
	return vm.ResourcePlan{RestartHost: host, RestartVM: host || r.DiskGiB != cur.DiskGiB}, nil
}

// SetResources writes the new values. It does not restart anything; see
// RestartHost (CPUs, memory) and Start (disk).
func (b *Backend) SetResources(ctx context.Context, r vm.Resources) error {
	if err := r.Validate(); err != nil {
		return err
	}
	if max := b.maxCPUs(); r.CPUs > max {
		return fmt.Errorf("cpus must be <= %d (the number of logical CPUs)", max)
	}
	content, cfg, err := b.readWSLConfig()
	if err != nil {
		return err
	}
	if cfg.Processors != r.CPUs || cfg.MemoryMiB != r.MemoryMiB {
		if content != "" {
			backup := fmt.Sprintf("%s.bak-%s", b.ConfigPath, b.now().Format("20060102-150405"))
			if err := os.WriteFile(backup, []byte(content), 0o644); err != nil {
				return fmt.Errorf("back up .wslconfig: %w", err)
			}
		}
		updated := updateWSLConfig(content, wslConfig{Processors: r.CPUs, MemoryMiB: r.MemoryMiB})
		if err := writeFileAtomic(b.ConfigPath, []byte(updated)); err != nil {
			return fmt.Errorf("write .wslconfig: %w", err)
		}
	}
	s, err := b.loadState()
	if err != nil {
		return err
	}
	if s.DiskGiB != r.DiskGiB {
		s.DiskGiB = r.DiskGiB
		if err := b.saveState(s); err != nil {
			return fmt.Errorf("save state: %w", err)
		}
	}
	return nil
}

// RestartHost shuts the whole WSL VM down (all distros of the user) so that
// .wslconfig takes effect, then starts the engine again if it was running.
func (b *Backend) RestartHost(ctx context.Context) error {
	b.mu.Lock()
	wasRunning := b.keepaliveRunning()
	b.mu.Unlock()
	_ = b.endSession()
	res, err := b.Runner.Run(ctx, "--shutdown")
	if err != nil {
		return fmt.Errorf("wsl --shutdown: %w", err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("wsl --shutdown: exit %d: %s", res.ExitCode, res.Stderr+res.Stdout)
	}
	if wasRunning {
		return b.Start(ctx)
	}
	return nil
}

// applyPendingDisk resizes the distro disk if a new limit was requested.
// The distro must be stopped.
func (b *Backend) applyPendingDisk(ctx context.Context) error {
	s, err := b.loadState()
	if err != nil {
		return err
	}
	if s.DiskGiB == s.AppliedDiskGiB || s.DiskGiB == 0 {
		return nil
	}
	res, err := b.Runner.Run(ctx, "--manage", b.Distro, "--resize", fmt.Sprintf("%dGB", s.DiskGiB))
	if err != nil {
		return fmt.Errorf("wsl --manage --resize: %w", err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("disk limit %d GB could not be applied: %s", s.DiskGiB, res.Stderr+res.Stdout)
	}
	s.AppliedDiskGiB = s.DiskGiB
	return b.saveState(s)
}

func (b *Backend) maxCPUs() int {
	if b.MaxCPUs > 0 {
		return b.MaxCPUs
	}
	return runtime.NumCPU()
}

func (b *Backend) now() time.Time {
	if b.Now != nil {
		return b.Now()
	}
	return time.Now()
}

// writeFileAtomic writes through a temp file and rename, so that a crash never
// leaves a half-written .wslconfig behind.
func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
}
