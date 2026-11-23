package core

import (
	"context"
	"strings"
	"testing"

	"github.com/tricknotes/merry-desktop/internal/vm"
	"github.com/tricknotes/merry-desktop/internal/vm/stub"
)

type networkBackend struct {
	*stub.Backend
	network  vm.Network
	applied  string
	psOutput string
	starts   int
}

func (b *networkBackend) Start(ctx context.Context) error {
	b.starts++
	return b.Backend.Start(ctx)
}

func (b *networkBackend) Exec(context.Context, []string) (vm.ExecResult, error) {
	return vm.ExecResult{Stdout: b.psOutput}, nil
}

func (b *networkBackend) CheckHost(context.Context) error                           { return nil }
func (b *networkBackend) DetectNetwork(context.Context) (vm.Network, error)         { return b.network, nil }
func (b *networkBackend) Provisioned(context.Context) (bool, error)                 { return true, nil }
func (b *networkBackend) Provision(context.Context, vm.Network, func(string)) error { return nil }
func (b *networkBackend) Destroy(context.Context) error                             { return nil }

func (b *networkBackend) ApplyNetwork(_ context.Context, n vm.Network) (bool, error) {
	changed := b.applied != n.ProxyURL
	b.applied = n.ProxyURL
	return changed, nil
}

func TestSyncNetwork(t *testing.T) {
	b := &networkBackend{Backend: stub.New(), network: vm.Network{ProxyURL: "http://p:3128"}, applied: "http://p:3128"}
	c := NewController(b, nil)
	ctx := context.Background()
	var notices []string
	c.OnNotice(func(m string) { notices = append(notices, m) })
	c.Start(ctx)

	if ch, err := c.SyncNetwork(ctx); err != nil || ch.Changed {
		t.Fatalf("unchanged: %+v, %v", ch, err)
	}

	// VPN off, a container runs: applied, not restarted.
	b.network.ProxyURL, b.psOutput = "", "abc123\n"
	ch, err := c.SyncNetwork(ctx)
	if err != nil || !ch.Changed || ch.Restarted || b.starts != 1 || len(notices) != 1 || !strings.Contains(notices[0], "トレイから再起動") {
		t.Fatalf("with containers: %+v, %v, starts=%d, notices=%q", ch, err, b.starts, notices)
	}

	// VPN on again, nothing runs: restarted.
	b.network.ProxyURL, b.psOutput = "http://p:3128", ""
	ch, err = c.SyncNetwork(ctx)
	if err != nil || !ch.Restarted || b.starts != 2 || c.Snapshot().Status != vm.StatusRunning {
		t.Fatalf("without containers: %+v, %v, starts=%d", ch, err, b.starts)
	}
}
