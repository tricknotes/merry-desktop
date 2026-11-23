package wsl

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tricknotes/merry-desktop/internal/mux"
	"github.com/tricknotes/merry-desktop/internal/vm"
)

const listOutput = `  NAME             STATE           VERSION
* Ubuntu           Running         2
  test-engine      %s         2
`

type fakeProcess struct {
	once sync.Once
	done chan struct{}
}

func (p *fakeProcess) Stop() error           { p.once.Do(func() { close(p.done) }); return nil }
func (p *fakeProcess) Done() <-chan struct{} { return p.done }

// fakeRunner answers wsl.exe invocations from a table of handlers.
type fakeRunner struct {
	mu        sync.Mutex
	state     string // STATE of test-engine; "" means not installed
	readyIn   int    // engine answers after this many checks
	checks    int
	calls     []string
	started   []*fakeProcess
	startErr  error
	resized   []string
	shutdowns int
	// handle answers calls before the defaults; ok=false falls through.
	handle func(opts RunOptions, args []string) (Result, bool)
	// agents are the agent ends of the sessions started with StartStdio.
	agents []*mux.Session
	// agentInstalls counts installs of the embedded engine-agent.
	agentInstalls int
	agentHash     string
	// integrations are the commands run in the user's distro (mount, context).
	integrations []string
}

func (f *fakeRunner) Run(ctx context.Context, args ...string) (Result, error) {
	return f.RunWith(ctx, RunOptions{}, args...)
}

func (f *fakeRunner) RunWith(_ context.Context, opts RunOptions, args ...string) (Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	joined := strings.Join(args, " ")
	f.calls = append(f.calls, joined)
	if f.handle != nil {
		if res, ok := f.handle(opts, args); ok {
			return res, nil
		}
	}
	switch {
	case joined == "--list --verbose":
		if f.state == "" {
			return Result{Stdout: strings.Replace(listOutput, "  test-engine      %s         2\n", "", 1)}, nil
		}
		return Result{Stdout: strings.Replace(listOutput, "%s", f.state, 1)}, nil
	case strings.HasSuffix(joined, "docker version --format {{.Server.Version}}"):
		f.checks++
		if f.state == "Running" && f.checks > f.readyIn {
			return Result{Stdout: "29.10.0\n"}, nil
		}
		return Result{ExitCode: 1, Stderr: "Cannot connect"}, nil
	case strings.HasSuffix(joined, "--exec sha256sum "+agentPath):
		if f.agentHash == "" {
			return Result{ExitCode: 1, Stderr: "No such file"}, nil
		}
		return Result{Stdout: f.agentHash + "  " + agentPath + "\n"}, nil
	case strings.Contains(joined, "gunzip -c > "+agentPath+".new"):
		f.agentInstalls++
		f.agentHash = agentSHA256
		return Result{}, nil
	case strings.HasPrefix(joined, "--distribution Ubuntu ") && strings.Contains(joined, "sh -c "):
		f.integrations = append(f.integrations, joined)
		return Result{}, nil
	case strings.HasPrefix(joined, "--manage test-engine --resize "):
		if f.state == "Running" {
			return Result{ExitCode: 1, Stderr: "distro is running"}, nil
		}
		f.resized = append(f.resized, strings.TrimPrefix(joined, "--manage test-engine --resize "))
		return Result{}, nil
	case joined == "--shutdown":
		f.shutdowns++
		if f.state != "" {
			f.state = "Stopped"
		}
		return Result{}, nil
	case strings.HasPrefix(joined, "--terminate "):
		f.state = "Stopped"
		return Result{}, nil
	case strings.HasPrefix(joined, "--distribution test-engine --user root --exec echo"):
		return Result{Stdout: strings.TrimPrefix(joined, "--distribution test-engine --user root --exec echo ") + "\n"}, nil
	case strings.HasPrefix(joined, "--distribution test-engine --user root --exec false"):
		return Result{ExitCode: 1}, nil
	}
	return Result{ExitCode: 127, Stderr: "unexpected: " + joined}, nil
}

// fakeStdio is the host end of a fake engine-agent session; agent is the other end.
type fakeStdio struct {
	*fakeProcess
	net.Conn
}

func (s *fakeStdio) Close() error { _ = s.Conn.Close(); return s.fakeProcess.Stop() }
func (s *fakeStdio) Stop() error  { _ = s.Conn.Close(); return s.fakeProcess.Stop() }

func (f *fakeRunner) StartStdio(args ...string) (StdioProcess, error) {
	p, err := f.Start(args...)
	if err != nil {
		return nil, err
	}
	host, agent := net.Pipe()
	f.mu.Lock()
	f.agents = append(f.agents, mux.Server(agent))
	f.mu.Unlock()
	return &fakeStdio{fakeProcess: p.(*fakeProcess), Conn: host}, nil
}

func (f *fakeRunner) Start(args ...string) (Process, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.startErr != nil {
		return nil, f.startErr
	}
	f.calls = append(f.calls, "start: "+strings.Join(args, " "))
	f.state = "Running"
	p := &fakeProcess{done: make(chan struct{})}
	f.started = append(f.started, p)
	return p, nil
}

func newTestBackend(t *testing.T, f *fakeRunner) *Backend {
	dir := t.TempDir()
	return &Backend{
		Distro: "test-engine", Runner: f, ReadyTimeout: time.Second, PollInterval: time.Millisecond,
		ConfigPath: filepath.Join(dir, ".wslconfig"), StatePath: filepath.Join(dir, "state", "wsl-state.json"),
		MaxCPUs: 8, Now: func() time.Time { return time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC) },
	}
}

func TestParseList(t *testing.T) {
	got := parseList("  NAME   STATE   VERSION\n* Ubuntu Running 2\n  eng Stopped 2\n\n")
	if len(got) != 2 {
		t.Fatalf("got %d distros, want 2: %+v", len(got), got)
	}
	if !got[0].Default || got[0].Name != "Ubuntu" || got[0].State != stateRunning {
		t.Errorf("first = %+v", got[0])
	}
	if got[1].Default || got[1].Name != "eng" || got[1].State != stateStopped || got[1].Version != "2" {
		t.Errorf("second = %+v", got[1])
	}
}

func TestStartWaitsForEngineAndKeepsSessionOpen(t *testing.T) {
	f := &fakeRunner{state: "Stopped", readyIn: 3}
	b := newTestBackend(t, f)
	if err := b.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if len(f.started) != 1 {
		t.Fatalf("keepalive sessions = %d, want 1", len(f.started))
	}
	want := "start: " + strings.Join(b.agentCommand([]string{"Ubuntu"}), " ")
	found := false
	for _, c := range f.calls {
		found = found || c == want
	}
	if !found {
		t.Errorf("agent session not started; calls = %v", f.calls)
	}
	if f.agentInstalls != 1 {
		t.Errorf("agent installs = %d, want 1", f.agentInstalls)
	}
	// A second Start must not open another session.
	if err := b.Start(context.Background()); err != nil {
		t.Fatalf("second Start: %v", err)
	}
	if len(f.started) != 1 {
		t.Errorf("keepalive sessions after second Start = %d, want 1", len(f.started))
	}
}

func TestAgentIsNotReinstalledWhenCurrent(t *testing.T) {
	f := &fakeRunner{state: "Stopped", agentHash: agentSHA256}
	if err := newTestBackend(t, f).Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.agentInstalls != 0 {
		t.Errorf("reinstalled an up-to-date agent")
	}
}

func TestDialEngineOpensAStreamWithMeta(t *testing.T) {
	f := &fakeRunner{state: "Stopped"}
	b := newTestBackend(t, f)
	if _, err := b.DialEngine("windows"); !errors.Is(err, vm.ErrNotRunning) {
		t.Fatalf("DialEngine before Start = %v, want ErrNotRunning", err)
	}
	if err := b.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	conn, err := b.DialEngine("windows")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	st, err := f.agents[0].Accept()
	if err != nil || string(st.Meta()) != "windows" {
		t.Fatalf("agent got %q, %v", st.Meta(), err)
	}
	if err := b.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := b.DialEngine("windows"); !errors.Is(err, vm.ErrNotRunning) {
		t.Fatalf("DialEngine after Stop = %v, want ErrNotRunning", err)
	}
}

func TestStartNotProvisioned(t *testing.T) {
	b := newTestBackend(t, &fakeRunner{})
	if err := b.Start(context.Background()); !errors.Is(err, vm.ErrNotProvisioned) {
		t.Fatalf("Start error = %v, want ErrNotProvisioned", err)
	}
}

func TestStartTimesOut(t *testing.T) {
	f := &fakeRunner{state: "Stopped", readyIn: 1 << 30}
	b := newTestBackend(t, f)
	b.ReadyTimeout = 20 * time.Millisecond
	err := b.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "did not respond") {
		t.Fatalf("Start error = %v, want timeout", err)
	}
}

func TestStopEndsKeepaliveAndTerminates(t *testing.T) {
	f := &fakeRunner{state: "Stopped"}
	b := newTestBackend(t, f)
	if err := b.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := b.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	select {
	case <-f.started[0].Done():
	default:
		t.Error("keepalive still running after Stop")
	}
	if f.calls[len(f.calls)-1] != "--terminate test-engine" {
		t.Errorf("last call = %q", f.calls[len(f.calls)-1])
	}
}

func TestStatus(t *testing.T) {
	cases := []struct {
		state   string
		readyIn int
		want    vm.Status
		wantErr error
	}{
		{state: "Stopped", want: vm.StatusStopped},
		{state: "Running", readyIn: 0, want: vm.StatusRunning},
		{state: "Running", readyIn: 5, want: vm.StatusStarting},
		{state: "Installing", want: vm.StatusStarting},
		{state: "", want: vm.StatusError, wantErr: vm.ErrNotProvisioned},
	}
	for _, c := range cases {
		b := newTestBackend(t, &fakeRunner{state: c.state, readyIn: c.readyIn})
		got, err := b.Status(context.Background())
		if got != c.want || !errors.Is(err, c.wantErr) {
			t.Errorf("state %q: Status = %q, %v; want %q, %v", c.state, got, err, c.want, c.wantErr)
		}
	}
}

func TestExec(t *testing.T) {
	b := newTestBackend(t, &fakeRunner{state: "Running"})
	res, err := b.Exec(context.Background(), []string{"echo", "hello"})
	if err != nil || res.ExitCode != 0 || res.Stdout != "hello\n" {
		t.Fatalf("Exec = %+v, %v", res, err)
	}
	res, err = b.Exec(context.Background(), []string{"false"})
	if err != nil || res.ExitCode != 1 {
		t.Fatalf("Exec false = %+v, %v; want exit 1 without error", res, err)
	}
	stopped := newTestBackend(t, &fakeRunner{state: "Stopped"})
	if _, err := stopped.Exec(context.Background(), []string{"echo"}); !errors.Is(err, vm.ErrNotRunning) {
		t.Fatalf("Exec on stopped VM error = %v, want ErrNotRunning", err)
	}
}

func TestCleanOutput(t *testing.T) {
	if got := cleanOutput("a\x00b\r\n"); got != "ab\n" {
		t.Errorf("cleanOutput = %q", got)
	}
}
