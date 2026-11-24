// Package wsl implements vm.Backend with a dedicated WSL2 distro that runs
// Docker Engine (see docs/phase0-wsl2-backend.md).
package wsl

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/tricknotes/merry-desktop/internal/mux"
	"github.com/tricknotes/merry-desktop/internal/vm"
)

// Backend drives one WSL2 distro through wsl.exe.
//
// WSL stops a distro shortly after its last wsl.exe session ends, which also
// stops Docker Engine. While the VM should run, Backend therefore keeps one
// session open (the engine-agent, see agent.go) and Stop ends it.
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

	// Provisioning (F-10). InstallRoot holds the distro disk and the download
	// cache (%LOCALAPPDATA%\<distro>). HTTPClient nil means a client that uses
	// the detected proxy.
	InstallRoot  string
	RootfsURL    string
	RootfsSHA256 string
	HTTPClient   *http.Client
	// HostCLI is where the Windows docker CLI is installed (F-04).
	HostCLI hostCLI
	// HostPath converts local paths for wsl.exe arguments (nil: unchanged).
	HostPath func(string) string
	// ContainerCA gives new containers the VM's CA bundle (F-11). Set it with
	// SetContainerCA while the VM runs.
	ContainerCA bool
	// IdleStop is how long the engine may be idle before the agent stops it
	// (F-06); 0 never stops it. Set it with SetIdleStop while the VM runs.
	IdleStop time.Duration

	// appliedNetwork is the fingerprint of the network settings last applied
	// to or found in the VM (C-2). Guarded by mu.
	appliedNetwork string

	// HostPorts lists the ports in use on the Windows host (nil: netstat.exe).
	HostPorts func(context.Context) ([]hostListener, error)
	// HostDockerConfig is the docker CLI config of the host user
	// (%USERPROFILE%\.docker\config.json); empty skips it.
	HostDockerConfig string
	// PipeNames are the named pipes tried in order for the host CLI (Windows;
	// nil: \\.\pipe\docker_engine, then one named after the distro).
	PipeNames []string

	// Distros overrides which WSL distros are integrated (nil: the default
	// distro only, see distros.go).
	Distros []string

	mu          sync.Mutex
	integrated  []string        // distros served by the running agent
	notice      func(string)    // receives the agent's notices
	contextDone map[string]bool // docker context checked in this run
	keepalive   StdioProcess    // the engine-agent session (see agent.go)
	session     *mux.Session
	endpoint    *endpoint // where the host docker CLI connects (Windows: a named pipe)
	asleep      bool      // the agent stopped the idle engine (F-06)
	onIdle      func(asleep bool)
}

var _ vm.Backend = (*Backend)(nil)

// New returns a Backend for the given distro using the real wsl.exe.
func New(distro string) *Backend {
	b := &Backend{
		Distro:       distro,
		Runner:       ExecRunner{},
		ReadyTimeout: 60 * time.Second,
		PollInterval: 250 * time.Millisecond,
		ConfigPath:   defaultConfigPath(),
		StatePath:    defaultStatePath(distro),
		InstallRoot:  defaultInstallRoot(distro),
		RootfsURL:    alpineURL(),
		RootfsSHA256: alpineSHA256,
	}
	b.HostCLI = defaultHostCLI(b.InstallRoot)
	if home, err := os.UserHomeDir(); err == nil {
		b.HostDockerConfig = filepath.Join(home, ".docker", "config.json")
	}
	return b
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

// endSession closes the host endpoint and the agent session.
func (b *Backend) endSession() error {
	b.stopEndpoint()
	b.setAsleep(false)
	b.mu.Lock()
	p, s := b.keepalive, b.session
	b.keepalive, b.session = nil, nil
	b.mu.Unlock()
	if s != nil {
		_ = s.Close()
	}
	if p != nil {
		if err := p.Stop(); err != nil {
			return fmt.Errorf("stop engine-agent: %w", err)
		}
	}
	return nil
}

// DialEngine opens a connection to the engine API through the agent session.
// meta names the kind of client, which decides the path translation.
func (b *Backend) DialEngine(meta string) (net.Conn, error) {
	b.mu.Lock()
	s := b.session
	b.mu.Unlock()
	if s == nil {
		return nil, vm.ErrNotRunning
	}
	return s.Open([]byte(meta))
}

func (b *Backend) listOrNil(ctx context.Context) []distro {
	res, err := b.Runner.Run(ctx, "--list", "--verbose")
	if err != nil || res.ExitCode != 0 {
		return nil
	}
	return parseList(res.Stdout)
}

// engineReady reports whether Docker Engine answers inside the distro.
func (b *Backend) engineReady(ctx context.Context) bool {
	res, err := b.Runner.Run(ctx, b.inDistro("docker", "version", "--format", "{{.Server.Version}}")...)
	return err == nil && res.ExitCode == 0 && strings.TrimSpace(res.Stdout) != ""
}

func (b *Backend) keepaliveRunning() bool {
	if b.keepalive == nil || b.session == nil {
		return false
	}
	select {
	case <-b.keepalive.Done():
		return false
	case <-b.session.Done():
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
	running := b.keepaliveRunning()
	b.mu.Unlock()
	if !running && d.State == stateRunning && !b.engineReady(ctx) {
		// The distro runs without the engine: a previous agent stopped it for
		// being idle (F-06). The boot command will not run again; start it.
		if err := b.mustRun(ctx, RunOptions{}, b.inDistro(engineStart)...); err != nil {
			return fmt.Errorf("start the engine: %w", err)
		}
	}
	if !running {
		// Running a command boots the distro; its [boot] command starts dockerd.
		if err := b.ensureAgent(ctx); err != nil {
			return err
		}
		list := b.listOrNil(ctx)
		b.mu.Lock()
		targets := b.integrationTargets(list)
		b.mu.Unlock()
		p, err := b.Runner.StartStdio(b.agentCommand(targets)...)
		if err != nil {
			return fmt.Errorf("start %s: %w", b.Distro, err)
		}
		session := mux.Client(p)
		b.mu.Lock()
		b.keepalive = p
		b.session = session
		b.integrated = targets
		b.mu.Unlock()
		go b.serveControl(session)
	}

	ctx, cancel := context.WithTimeout(ctx, b.ReadyTimeout)
	defer cancel()
	for {
		if b.engineReady(ctx) {
			// Best effort: a distro that cannot be integrated must not stop the engine.
			_ = b.Maintain(ctx)
			return b.startEndpoint()
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
	if err := b.endSession(); err != nil {
		return err
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
	if b.isAsleep() {
		// The command may use the engine; start it like a client request does.
		if err := b.wake(ctx); err != nil {
			return vm.ExecResult{}, err
		}
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
		if b.isAsleep() || b.engineReady(ctx) {
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
