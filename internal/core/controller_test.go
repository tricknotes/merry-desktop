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

func TestApplyResourcesValidates(t *testing.T) {
	c := NewController(newStub(), nil)
	ctx := context.Background()
	if err := c.ApplyResources(ctx, vm.Resources{CPUs: -1}, false); err == nil {
		t.Fatal("want validation error")
	}
	ok := vm.Resources{CPUs: 4, MemoryMiB: 8192}
	if err := c.ApplyResources(ctx, ok, false); err != nil {
		t.Fatal(err)
	}
	if got := c.Snapshot().Resources; got != ok {
		t.Fatalf("resources = %+v", got)
	}
}

// managed is a stub with a ResourceManager whose plan is fixed.
type managed struct {
	*stub.Backend
	plan         vm.ResourcePlan
	hostRestarts int
	current      vm.Resources
}

func (m *managed) CurrentResources(context.Context) (vm.Resources, error) { return m.current, nil }
func (m *managed) PlanResources(context.Context, vm.Resources) (vm.ResourcePlan, error) {
	return m.plan, nil
}
func (m *managed) RestartHost(ctx context.Context) error {
	m.hostRestarts++
	if err := m.Backend.Stop(ctx); err != nil {
		return err
	}
	return m.Backend.Start(ctx)
}

func TestApplyResourcesRestartsHostOnlyWhenAsked(t *testing.T) {
	m := &managed{Backend: newStub(), plan: vm.ResourcePlan{RestartVM: true, RestartHost: true}}
	c := NewController(m, nil)
	ctx := context.Background()
	c.Start(ctx)

	if err := c.ApplyResources(ctx, vm.Resources{CPUs: 2}, false); err != nil {
		t.Fatal(err)
	}
	if m.hostRestarts != 0 {
		t.Fatalf("restarted without being asked")
	}
	if err := c.ApplyResources(ctx, vm.Resources{CPUs: 4}, true); err != nil {
		t.Fatal(err)
	}
	if m.hostRestarts != 1 {
		t.Fatalf("hostRestarts = %d, want 1", m.hostRestarts)
	}
	if s := c.Snapshot(); s.Status != vm.StatusRunning {
		t.Fatalf("status after host restart = %q", s.Status)
	}
}

func TestApplyResourcesRestartsOnlyTheVM(t *testing.T) {
	m := &managed{Backend: newStub(), plan: vm.ResourcePlan{RestartVM: true}}
	c := NewController(m, nil)
	rec := &recorder{}
	ctx := context.Background()
	c.Start(ctx)
	c.OnChange(rec.add)
	if err := c.ApplyResources(ctx, vm.Resources{DiskGiB: 64}, true); err != nil {
		t.Fatal(err)
	}
	want := []vm.Status{vm.StatusRunning, vm.StatusStopped, vm.StatusStarting, vm.StatusRunning}
	if got := rec.get(); !equal(got, want) || m.hostRestarts != 0 {
		t.Fatalf("statuses = %v (host restarts %d), want %v", got, m.hostRestarts, want)
	}
}

func TestLoadResources(t *testing.T) {
	m := &managed{Backend: newStub(), current: vm.Resources{CPUs: 6, MemoryMiB: 12288}}
	c := NewController(m, nil)
	if err := c.LoadResources(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := c.Snapshot().Resources; got != m.current {
		t.Fatalf("resources = %+v", got)
	}
}

func TestUserOperationsWaitForRefresh(t *testing.T) {
	b := stub.New()
	b.StartDelay = 0
	c := NewController(b, nil)
	c.begin() // stands in for a Refresh in progress
	done := make(chan struct{})
	go func() {
		c.Start(context.Background())
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("Start did not wait for the running operation")
	case <-time.After(30 * time.Millisecond):
	}
	c.end()
	<-done
	if s := c.Snapshot(); s.Status != vm.StatusRunning {
		t.Fatalf("Start was dropped instead of waiting: %q", s.Status)
	}
}

func TestRefreshSkipsWhileAnOperationRuns(t *testing.T) {
	c := NewController(newStub(), nil)
	c.begin()
	defer c.end()
	finished := make(chan struct{})
	go func() {
		c.Refresh(context.Background())
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("Refresh waited instead of skipping")
	}
}

// countingStub counts Start calls on the backend.
type countingStub struct {
	*stub.Backend
	starts int
}

func (s *countingStub) Start(ctx context.Context) error {
	s.starts++
	return s.Backend.Start(ctx)
}

func TestStartConnectsToAVMStartedElsewhere(t *testing.T) {
	b := &countingStub{Backend: newStub()}
	_ = b.Backend.Start(context.Background()) // running, but not started by this controller
	c := NewController(b, nil)
	c.Refresh(context.Background())
	if c.Snapshot().Status != vm.StatusRunning {
		t.Fatal("refresh did not see the running VM")
	}
	c.Start(context.Background())
	if b.starts != 1 {
		t.Fatalf("backend starts = %d, want 1 (this process must connect)", b.starts)
	}
	c.Start(context.Background())
	if b.starts != 1 {
		t.Fatalf("backend starts = %d after a second Start, want still 1", b.starts)
	}
}
