package main

import (
	"log"
	"sync"
)

// BenchRevision is changed by the benchmark script to measure incremental backend builds.
const BenchRevision = 0

// Snapshot is the UI-facing state.
type Snapshot struct {
	Status    VmStatus  `json:"status"`
	Resources Resources `json:"resources"`
}

// VmController is the OS-independent core: it owns the UI-facing state and drives the backend.
type VmController struct {
	backend  VmBackend
	listener func(VmStatus)

	mu    sync.Mutex
	state Snapshot
}

func NewVmController(backend VmBackend) *VmController {
	return &VmController{
		backend:  backend,
		listener: func(VmStatus) {},
		state:    Snapshot{Status: StatusStopped, Resources: Resources{CPUs: 2, MemoryMiB: 4096}},
	}
}

// OnStatus registers the single status listener. Call before Start/Stop.
func (c *VmController) OnStatus(listener func(VmStatus)) {
	c.listener = listener
}

func (c *VmController) Snapshot() Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

// Start blocks until the backend finishes starting. Call from a goroutine.
func (c *VmController) Start() {
	c.mu.Lock()
	if c.state.Status == StatusStarting || c.state.Status == StatusRunning {
		c.mu.Unlock()
		return
	}
	c.state.Status = StatusStarting
	c.mu.Unlock()
	c.listener(StatusStarting)

	next := StatusRunning
	if err := c.backend.Start(); err != nil {
		log.Println(err)
		next = StatusError
	}
	c.setStatus(next)
}

func (c *VmController) Stop() {
	next := StatusStopped
	if err := c.backend.Stop(); err != nil {
		log.Println(err)
		next = StatusError
	}
	c.setStatus(next)
}

func (c *VmController) SetResources(resources Resources) error {
	if err := c.backend.SetResources(resources); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.state.Resources = resources
	return nil
}

func (c *VmController) setStatus(status VmStatus) {
	c.mu.Lock()
	c.state.Status = status
	c.mu.Unlock()
	c.listener(status)
}
