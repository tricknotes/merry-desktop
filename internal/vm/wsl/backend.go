// Package wsl implements vm.Backend with a dedicated WSL2 distro that runs
// Docker Engine (see docs/phase0-wsl2-backend.md).
package wsl

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/tricknotes/merry-desktop/internal/vm"
)

// Backend drives one WSL2 distro through wsl.exe.
//
// WSL stops a distro shortly after its last wsl.exe session ends, which also
// stops Docker Engine. While the VM should run, Backend therefore keeps one
// session open (the keepalive) and Stop ends it.
type Backend struct {
	Distro string
	Runner Runner
	// ReadyTimeout bounds how long Start waits for the engine.
	ReadyTimeout time.Duration
	// PollInterval is the wait between engine checks during Start.
	PollInterval time.Duration
	// ConfigPath is %UserProfile%\.wslconfig. StatePath stores the disk limit.
	ConfigPath string
	StatePath  string
	// MaxCPUs and Now are for tests; zero values mean the real machine.
	MaxCPUs int
	Now     func() time.Time

	mu        sync.Mutex
	keepalive Process
}

var _ vm.Backend = (*Backend)(nil)

// New returns a Backend for the given distro using the real wsl.exe.
func New(distro string) *Backend {
	return &Backend{
		Distro:       distro,
		Runner:       ExecRunner{},
		ReadyTimeout: 60 * time.Second,
		PollInterval: 250 * time.Millisecond,
		ConfigPath:   defaultConfigPath(),
		StatePath:    defaultStatePath(distro),
	}
}

func (b *Backend) inDistro(args ...string) []string {
	return append([]string{"--distribution", b.Distro, "--user", "root", "--exec"}, args...)
}

func (b *Backend) lookup(ctx context.Context) (distro, error) {
	res, err := b.Runner.Run(ctx, "--list", "--verbose")
	if err != nil {
		return distro{}, fmt.Errorf("wsl --list: %w", err)
	}
	if res.ExitCode != 0 {
		// wsl.exe exits non-zero when no distro is installed at all.
		return distro{}, vm.ErrNotProvisioned
	}
	d, ok := findDistro(parseList(res.Stdout), b.Distro)
	if !ok {
		return distro{}, vm.ErrNotProvisioned
	}
	return d, nil
}

// engineReady reports whether Docker Engine answers inside the distro.
func (b *Backend) engineReady(ctx context.Context) bool {
	res, err := b.Runner.Run(ctx, b.inDistro("docker", "version", "--format", "{{.Server.Version}}")...)
	return err == nil && res.ExitCode == 0 && strings.TrimSpace(res.Stdout) != ""
}

func (b *Backend) keepaliveRunning() bool {
	if b.keepalive == nil {
		return false
	}
	select {
	case <-b.keepalive.Done():
		return false
	default:
		return true
	}
}

func (b *Backend) Start(ctx context.Context) error {
	d, err := b.lookup(ctx)
	if err != nil {
		return err
	}
	if d.State == stateStopped {
		// The disk limit can only change while the distro is stopped (F-03).
		if err := b.applyPendingDisk(ctx); err != nil {
			return err
		}
	}

	b.mu.Lock()
	if !b.keepaliveRunning() {
		// Starting a session boots the distro; its [boot] command starts dockerd.
		p, err := b.Runner.Start(b.inDistro("sleep", "infinity")...)
		if err != nil {
			b.mu.Unlock()
			return fmt.Errorf("start %s: %w", b.Distro, err)
		}
		b.keepalive = p
	}
	b.mu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, b.ReadyTimeout)
	defer cancel()
	for {
		if b.engineReady(ctx) {
			return nil
		}
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return fmt.Errorf("container engine in %s did not respond within %s", b.Distro, b.ReadyTimeout)
			}
			return ctx.Err()
		case <-time.After(b.PollInterval):
		}
	}
}

func (b *Backend) Stop(ctx context.Context) error {
	b.mu.Lock()
	p := b.keepalive
	b.keepalive = nil
	b.mu.Unlock()
	if p != nil {
		if err := p.Stop(); err != nil {
			return fmt.Errorf("stop keepalive: %w", err)
		}
	}
	res, err := b.Runner.Run(ctx, "--terminate", b.Distro)
	if err != nil {
		return fmt.Errorf("wsl --terminate: %w", err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("wsl --terminate %s: exit %d: %s", b.Distro, res.ExitCode, strings.TrimSpace(res.Stderr+res.Stdout))
	}
	return nil
}

func (b *Backend) Exec(ctx context.Context, args []string) (vm.ExecResult, error) {
	if len(args) == 0 {
		return vm.ExecResult{}, errors.New("exec: no command")
	}
	d, err := b.lookup(ctx)
	if err != nil {
		return vm.ExecResult{}, err
	}
	if d.State != stateRunning {
		return vm.ExecResult{}, vm.ErrNotRunning
	}
	res, err := b.Runner.Run(ctx, b.inDistro(args...)...)
	if err != nil {
		return vm.ExecResult{}, err
	}
	return vm.ExecResult{ExitCode: res.ExitCode, Stdout: res.Stdout, Stderr: res.Stderr}, nil
}

func (b *Backend) Status(ctx context.Context) (vm.Status, error) {
	d, err := b.lookup(ctx)
	if err != nil {
		return vm.StatusError, err
	}
	switch d.State {
	case stateRunning:
		if b.engineReady(ctx) {
			return vm.StatusRunning, nil
		}
		return vm.StatusStarting, nil
	case stateStopped:
		return vm.StatusStopped, nil
	default:
		// Installing, converting or uninstalling: not usable right now.
		return vm.StatusStarting, nil
	}
}
