// Package stub is an in-memory vm.Backend for development and tests.
// No VM is touched.
package stub

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/tricknotes/merry-desktop/internal/vm"
)

// Backend simulates a VM. StartDelay and FailStart control Start.
type Backend struct {
	StartDelay time.Duration
	FailStart  bool

	mu        sync.Mutex
	status    vm.Status
	resources vm.Resources
}

var _ vm.Backend = (*Backend)(nil)

func New() *Backend {
	return &Backend{
		StartDelay: 1500 * time.Millisecond,
		status:     vm.StatusStopped,
		resources:  vm.Resources{CPUs: 2, MemoryMiB: 4096},
	}
}

func (b *Backend) set(s vm.Status) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.status = s
}

func (b *Backend) Start(ctx context.Context) error {
	b.set(vm.StatusStarting)
	select {
	case <-time.After(b.StartDelay):
	case <-ctx.Done():
		b.set(vm.StatusStopped)
		return ctx.Err()
	}
	if b.FailStart {
		b.set(vm.StatusError)
		return errors.New("stub: start failed")
	}
	b.set(vm.StatusRunning)
	return nil
}

func (b *Backend) Stop(context.Context) error {
	b.set(vm.StatusStopped)
	return nil
}

func (b *Backend) Exec(_ context.Context, args []string) (vm.ExecResult, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.status != vm.StatusRunning {
		return vm.ExecResult{}, vm.ErrNotRunning
	}
	return vm.ExecResult{Stdout: "stub: " + strings.Join(args, " ")}, nil
}

func (b *Backend) Status(context.Context) (vm.Status, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.status, nil
}

func (b *Backend) SetResources(_ context.Context, r vm.Resources) error {
	if err := r.Validate(); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.resources = r
	return nil
}
