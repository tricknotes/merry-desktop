package wsl

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/tricknotes/merry-desktop/internal/vm"
)

// Idle stop (F-06) is done by the engine-agent in the VM: it stops dockerd
// when idle and starts it for the next request. The distro keeps running, so
// that the docker CLI in the integrated distros, whose sockets the agent
// serves, can wake the engine too.

var _ vm.IdleStopper = (*Backend)(nil)

// SetIdleStop changes the idle time. While the VM runs, only the agent
// session is reconnected; containers keep running.
func (b *Backend) SetIdleStop(ctx context.Context, after time.Duration) error {
	if after < 0 {
		return fmt.Errorf("invalid idle time %s", after)
	}
	b.mu.Lock()
	same := b.IdleStop == after
	b.IdleStop = after
	running := b.keepaliveRunning()
	b.mu.Unlock()
	if same || !running {
		return nil
	}
	if err := b.endSession(); err != nil {
		return err
	}
	return b.Start(ctx)
}

// OnIdle sets the receiver of the agent's idle changes.
func (b *Backend) OnIdle(f func(asleep bool)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.onIdle = f
}

func (b *Backend) setAsleep(asleep bool) {
	b.mu.Lock()
	changed := b.asleep != asleep
	b.asleep = asleep
	f := b.onIdle
	b.mu.Unlock()
	if changed && f != nil {
		f(asleep)
	}
}

func (b *Backend) isAsleep() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.asleep
}

// wake makes the agent start the engine, by sending it a request.
func (b *Backend) wake(ctx context.Context) error {
	client := &http.Client{Transport: &http.Transport{
		DialContext: func(context.Context, string, string) (net.Conn, error) { return b.DialEngine("") },
	}}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker/_ping", nil)
	if err != nil {
		return err
	}
	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("start the engine: %w", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("start the engine: %s", res.Status)
	}
	b.setAsleep(false)
	return nil
}
