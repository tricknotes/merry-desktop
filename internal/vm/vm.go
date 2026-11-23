// Package vm defines the OS-independent contract for the VM that runs the
// container engine. Only implementations of Backend may depend on the OS.
//
// Implementations must not leak backend-specific concepts (WSL distro names,
// VHDX paths, Lima instance names) through this package.
package vm

import (
	"context"
	"errors"
)

// Status is the state of the VM as seen by the user.
type Status string

const (
	StatusStopped  Status = "stopped"
	StatusStarting Status = "starting"
	StatusRunning  Status = "running"
	StatusError    Status = "error"
)

// Resources is what the VM may use.
type Resources struct {
	CPUs      int `json:"cpus"`
	MemoryMiB int `json:"memoryMiB"`
}

// ExecResult is the outcome of a command run inside the VM.
type ExecResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

var (
	// ErrNotRunning is returned by Exec when the VM is not running.
	ErrNotRunning = errors.New("vm is not running")
	// ErrNotProvisioned is returned when the VM does not exist yet.
	ErrNotProvisioned = errors.New("vm is not set up")
	// ErrNotSupported is returned for operations a backend cannot do (yet).
	ErrNotSupported = errors.New("not supported by this backend")
)

// Backend holds the OS-specific VM operations.
type Backend interface {
	// Start boots the VM and returns once the container engine responds.
	Start(ctx context.Context) error
	// Stop shuts the VM down.
	Stop(ctx context.Context) error
	// Exec runs a command inside the VM as root.
	Exec(ctx context.Context, args []string) (ExecResult, error)
	// Status reports the current state.
	Status(ctx context.Context) (Status, error)
	// SetResources changes what the VM may use. It may require a restart.
	SetResources(ctx context.Context, resources Resources) error
}

// Validate checks values that every backend requires.
func (r Resources) Validate() error {
	if r.CPUs < 1 {
		return errors.New("cpus must be >= 1")
	}
	if r.MemoryMiB < 1024 {
		return errors.New("memoryMiB must be >= 1024")
	}
	return nil
}
