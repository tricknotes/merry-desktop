// Package core is the OS-independent part of the app: it owns the state that
// the UI shows and drives the VM backend.
package core

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"github.com/tricknotes/merry-desktop/internal/vm"
)

// Snapshot is the state shown by the UI.
type Snapshot struct {
	Status    vm.Status    `json:"status"`
	Resources vm.Resources `json:"resources"`
	// Message explains an error status in user-facing words. Empty otherwise.
	Message string `json:"message"`
}

// Controller serializes VM operations and tells listeners about status changes.
type Controller struct {
	backend vm.Backend
	log     *slog.Logger

	mu        sync.Mutex
	busy      bool // an operation is in flight
	state     Snapshot
	listeners []func(Snapshot)
}

func NewController(backend vm.Backend, log *slog.Logger) *Controller {
	if log == nil {
		log = slog.Default()
	}
	return &Controller{
		backend: backend,
		log:     log,
		state:   Snapshot{Status: vm.StatusStopped, Resources: vm.Resources{CPUs: 2, MemoryMiB: 4096}},
	}
}

// OnChange registers a listener. Listeners run on the goroutine that changed
// the state and must not block.
func (c *Controller) OnChange(listener func(Snapshot)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.listeners = append(c.listeners, listener)
}

func (c *Controller) Snapshot() Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

func (c *Controller) update(change func(*Snapshot)) {
	c.mu.Lock()
	change(&c.state)
	s := c.state
	listeners := append([]func(Snapshot){}, c.listeners...)
	c.mu.Unlock()
	for _, l := range listeners {
		l(s)
	}
}

func (c *Controller) setStatus(status vm.Status, message string) {
	c.update(func(s *Snapshot) { s.Status, s.Message = status, message })
}

// begin marks an operation as in flight. It returns false if one already is.
func (c *Controller) begin() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.busy {
		return false
	}
	c.busy = true
	return true
}

func (c *Controller) end() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.busy = false
}

// Start boots the VM. It blocks until the engine answers or start fails.
func (c *Controller) Start(ctx context.Context) {
	if !c.begin() {
		return
	}
	defer c.end()
	c.start(ctx)
}

func (c *Controller) start(ctx context.Context) {
	c.setStatus(vm.StatusStarting, "")
	if err := c.backend.Start(ctx); err != nil {
		c.log.Error("start failed", "err", err)
		c.setStatus(vm.StatusError, describe(err))
		return
	}
	c.setStatus(vm.StatusRunning, "")
}

// Stop shuts the VM down.
func (c *Controller) Stop(ctx context.Context) {
	if !c.begin() {
		return
	}
	defer c.end()
	c.stop(ctx)
}

func (c *Controller) stop(ctx context.Context) bool {
	if err := c.backend.Stop(ctx); err != nil {
		c.log.Error("stop failed", "err", err)
		c.setStatus(vm.StatusError, describe(err))
		return false
	}
	c.setStatus(vm.StatusStopped, "")
	return true
}

// Restart stops and starts the VM (F-01).
func (c *Controller) Restart(ctx context.Context) {
	if !c.begin() {
		return
	}
	defer c.end()
	if c.stop(ctx) {
		c.start(ctx)
	}
}

// Refresh reads the status from the backend, to notice changes made outside
// the app (for example `wsl --shutdown`). It does nothing while an operation runs.
func (c *Controller) Refresh(ctx context.Context) {
	if !c.begin() {
		return
	}
	defer c.end()
	status, err := c.backend.Status(ctx)
	if err != nil {
		c.setStatus(vm.StatusError, describe(err))
		return
	}
	if c.Snapshot().Status != status {
		c.setStatus(status, "")
	}
}

// SetResources stores new resource limits through the backend.
func (c *Controller) SetResources(ctx context.Context, r vm.Resources) error {
	if err := c.backend.SetResources(ctx, r); err != nil {
		return err
	}
	c.update(func(s *Snapshot) { s.Resources = r })
	return nil
}

// describe turns backend errors into user-facing messages (Japanese UI).
func describe(err error) string {
	switch {
	case errors.Is(err, vm.ErrNotProvisioned):
		return "VM がまだ用意されていません。セットアップを実行してください。"
	case errors.Is(err, context.DeadlineExceeded):
		return "時間内に起動しませんでした。"
	default:
		return "操作に失敗しました: " + err.Error()
	}
}
