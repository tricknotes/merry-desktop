package wsl

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/Microsoft/go-winio"
)

func uniquePipe(t *testing.T, suffix string) string {
	return fmt.Sprintf(`\\.\pipe\merry-test-%d-%s`, time.Now().UnixNano(), suffix)
}

// TestNamedPipeRelaysToTheAgent connects to the real named pipe and checks that
// bytes reach the agent end of the session, tagged as a Windows client.
func TestNamedPipeRelaysToTheAgent(t *testing.T) {
	f := &fakeRunner{state: "Stopped"}
	b := newTestBackend(t, f)
	pipe := uniquePipe(t, "relay")
	b.PipeNames = []string{pipe}
	if err := b.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer b.Stop(context.Background())
	if got, want := b.HostEndpoint(), "npipe:////./pipe/"+pipe[len(`\\.\pipe\`):]; got != want {
		t.Errorf("HostEndpoint = %q, want %q", got, want)
	}

	go func() {
		st, err := f.agents[0].Accept()
		if err != nil {
			return
		}
		if string(st.Meta()) != metaWindows {
			_, _ = st.Write([]byte("wrong meta: " + string(st.Meta())))
			_ = st.Close()
			return
		}
		buf := make([]byte, 4)
		_, _ = io.ReadFull(st, buf)
		_, _ = st.Write([]byte("pong:" + string(buf)))
		_ = st.CloseWrite()
	}()

	timeout := 5 * time.Second
	conn, err := winio.DialPipe(pipe, &timeout)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_, _ = conn.Write([]byte("ping"))
	got, _ := io.ReadAll(conn)
	if string(got) != "pong:ping" {
		t.Fatalf("got %q", got)
	}
}

// TestNamedPipeFallsBackWhenTaken checks the second name is used when another
// program already listens on the first (as with \\.\pipe\docker_engine).
func TestNamedPipeFallsBackWhenTaken(t *testing.T) {
	taken := uniquePipe(t, "taken")
	other, err := winio.ListenPipe(taken, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()

	f := &fakeRunner{state: "Stopped"}
	b := newTestBackend(t, f)
	fallback := uniquePipe(t, "fallback")
	b.PipeNames = []string{taken, fallback}
	if err := b.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer b.Stop(context.Background())
	if got := b.HostEndpoint(); got != "npipe:////./pipe/"+fallback[len(`\\.\pipe\`):] {
		t.Fatalf("HostEndpoint = %q, want the fallback pipe", got)
	}
	// After Stop, the pipe is closed.
	_ = b.Stop(context.Background())
	timeout := 500 * time.Millisecond
	if c, err := winio.DialPipe(fallback, &timeout); err == nil {
		c.Close()
		t.Fatal("pipe still accepts connections after Stop")
	}
}
