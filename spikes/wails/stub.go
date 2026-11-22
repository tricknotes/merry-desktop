package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

const stubStartDelay = 1500 * time.Millisecond

// StubBackend is an in-memory backend used by the spike. No VM is touched.
type StubBackend struct {
	mu        sync.Mutex
	state     VmStatus
	resources Resources
}

func NewStubBackend() *StubBackend {
	return &StubBackend{state: StatusStopped, resources: Resources{CPUs: 2, MemoryMiB: 4096}}
}

func (b *StubBackend) setState(status VmStatus) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.state = status
}

func (b *StubBackend) Start() error {
	b.setState(StatusStarting)
	time.Sleep(stubStartDelay)
	if os.Getenv("SPIKE_STUB_FAIL_START") == "1" {
		b.setState(StatusError)
		return errors.New("stub: start failed (SPIKE_STUB_FAIL_START=1)")
	}
	b.setState(StatusRunning)
	return nil
}

func (b *StubBackend) Stop() error {
	b.setState(StatusStopped)
	return nil
}

func (b *StubBackend) Exec(args []string) (ExecResult, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state != StatusRunning {
		return ExecResult{}, ErrNotRunning
	}
	return ExecResult{Stdout: fmt.Sprintf("stub: %s", strings.Join(args, " "))}, nil
}

func (b *StubBackend) Status() (VmStatus, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state, nil
}

func (b *StubBackend) SetResources(resources Resources) error {
	if resources.CPUs < 1 {
		return errors.New("cpus must be >= 1")
	}
	if resources.MemoryMiB < 1024 {
		return errors.New("memoryMiB must be >= 1024")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.resources = resources
	return nil
}
