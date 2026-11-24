package wsl

import (
	"bufio"
	"context"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tricknotes/merry-desktop/internal/vm"
)

func TestAgentCommandPassesIdleStop(t *testing.T) {
	b := newTestBackend(t, &fakeRunner{})
	if slices.Contains(b.agentCommand(nil), "--idle-stop") {
		t.Fatal("idle stop passed while off")
	}
	b.IdleStop = 10 * time.Minute
	cmd := b.agentCommand(nil)
	i := slices.Index(cmd, "--idle-stop")
	if i < 0 || cmd[i+1] != "10m0s" {
		t.Fatalf("agent command = %v", cmd)
	}
}

func TestIdleNoticeAndWakeOnExec(t *testing.T) {
	f := &fakeRunner{state: "Stopped"}
	b := newTestBackend(t, f)
	var mu sync.Mutex
	var events []bool
	b.OnIdle(func(asleep bool) { mu.Lock(); events = append(events, asleep); mu.Unlock() })
	ctx := context.Background()
	if err := b.Start(ctx); err != nil {
		t.Fatal(err)
	}
	agent := f.agents[0]

	// The agent stopped the engine.
	st, err := agent.Open([]byte(controlIdle + "asleep"))
	if err != nil {
		t.Fatal(err)
	}
	_ = st.CloseWrite()
	_ = st.Close()
	waitFor(t, func() bool { return b.isAsleep() })
	f.mu.Lock()
	f.readyIn = 1 << 30 // dockerd is not running
	f.mu.Unlock()
	if s, err := b.Status(ctx); s != vm.StatusRunning || err != nil {
		t.Fatalf("Status while asleep = %q, %v; want running", s, err)
	}

	// A command in the VM wakes it with a request through the agent first.
	pinged := make(chan string, 1)
	go func() {
		st, err := agent.Accept()
		if err != nil {
			return
		}
		defer st.Close()
		req, err := http.ReadRequest(bufio.NewReader(st))
		if err != nil {
			return
		}
		pinged <- req.URL.Path
		_, _ = st.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nOK"))
	}()
	res, err := b.Exec(ctx, []string{"echo", "hi"})
	if err != nil || strings.TrimSpace(res.Stdout) != "hi" {
		t.Fatalf("Exec = %+v, %v", res, err)
	}
	if p := <-pinged; p != "/_ping" {
		t.Fatalf("wake request = %q", p)
	}
	if b.isAsleep() {
		t.Fatal("still asleep after the wake")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(events) != 2 || !events[0] || events[1] {
		t.Fatalf("events = %v, want [asleep awake]", events)
	}
}

func TestStartStartsAnEngineStoppedByAPreviousAgent(t *testing.T) {
	f := &fakeRunner{state: "Running", readyIn: 1 << 30}
	f.handle = func(_ RunOptions, args []string) (Result, bool) {
		if args[len(args)-1] == engineStart {
			f.readyIn = f.checks // the engine answers from the next check
			return Result{}, true
		}
		return Result{}, false
	}
	b := newTestBackend(t, f)
	if err := b.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(f.calls, func(c string) bool { return strings.HasSuffix(c, "--exec "+engineStart) }) {
		t.Fatalf("engine-start not run; calls = %v", f.calls)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(time.Millisecond)
	}
}
