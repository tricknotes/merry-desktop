package core

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/tricknotes/merry-desktop/internal/vm"
	"github.com/tricknotes/merry-desktop/internal/vm/stub"
)

type recorder struct {
	mu       sync.Mutex
	statuses []vm.Status
}

func (r *recorder) add(s Snapshot) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.statuses = append(r.statuses, s.Status)
}

func (r *recorder) get() []vm.Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]vm.Status{}, r.statuses...)
}

func equal(a, b []vm.Status) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func newStub() *stub.Backend {
	b := stub.New()
	b.StartDelay = 0
	return b
}

func TestStartStopRestart(t *testing.T) {
	c := NewController(newStub(), nil)
	rec := &recorder{}
	c.OnChange(rec.add)
	ctx := context.Background()

	c.Start(ctx)
	c.Restart(ctx)
	c.Stop(ctx)

	want := []vm.Status{
		vm.StatusStarting, vm.StatusRunning, // Start
		vm.StatusStopped, vm.StatusStarting, vm.StatusRunning, // Restart
		vm.StatusStopped, // Stop
	}
	if got := rec.get(); !equal(got, want) {
		t.Fatalf("statuses = %v, want %v", got, want)
	}
}

func TestStartFailureShowsError(t *testing.T) {
	b := newStub()
	b.FailStart = true
	c := NewController(b, nil)
	c.Start(context.Background())
	s := c.Snapshot()
	if s.Status != vm.StatusError || s.Message == "" {
		t.Fatalf("snapshot = %+v, want error with message", s)
	}
}

type notProvisioned struct{ stub.Backend }

func (*notProvisioned) Start(context.Context) error { return vm.ErrNotProvisioned }

func TestNotProvisionedMessage(t *testing.T) {
	c := NewController(&notProvisioned{}, nil)
	c.Start(context.Background())
	if s := c.Snapshot(); s.Status != vm.StatusError || s.Message != describe(vm.ErrNotProvisioned) {
		t.Fatalf("snapshot = %+v", s)
	}
}

func TestOperationsDoNotOverlap(t *testing.T) {
	b := stub.New()
	b.StartDelay = 50 * time.Millisecond
	c := NewController(b, nil)
	rec := &recorder{}
	c.OnChange(rec.add)

	var wg sync.WaitGroup
	for range 3 {
		wg.Add(1)
		go func() { defer wg.Done(); c.Start(context.Background()) }()
	}
	wg.Wait()
	if got, want := rec.get(), []vm.Status{vm.StatusStarting, vm.StatusRunning}; !equal(got, want) {
		t.Fatalf("statuses = %v, want %v (one start only)", got, want)
	}
}

func TestRefreshPicksUpExternalChange(t *testing.T) {
	b := newStub()
	c := NewController(b, nil)
	ctx := context.Background()
	c.Start(ctx)
	_ = b.Stop(ctx) // stopped outside the controller
	c.Refresh(ctx)
	if s := c.Snapshot(); s.Status != vm.StatusStopped {
		t.Fatalf("status after refresh = %q, want stopped", s.Status)
	}
}

func TestSetResourcesValidates(t *testing.T) {
	c := NewController(newStub(), nil)
	if err := c.SetResources(context.Background(), vm.Resources{CPUs: 0, MemoryMiB: 4096}); err == nil {
		t.Fatal("want validation error")
	}
	ok := vm.Resources{CPUs: 4, MemoryMiB: 8192}
	if err := c.SetResources(context.Background(), ok); err != nil {
		t.Fatal(err)
	}
	if got := c.Snapshot().Resources; got != ok {
		t.Fatalf("resources = %+v", got)
	}
}
