package main

import "errors"

// VmStatus is the VM state as reported by a backend.
type VmStatus string

const (
	StatusStopped  VmStatus = "stopped"
	StatusStarting VmStatus = "starting"
	StatusRunning  VmStatus = "running"
	StatusError    VmStatus = "error"
)

// Resources is the CPU and memory assigned to the VM.
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

// ErrNotRunning is returned by Exec when the VM is not running.
var ErrNotRunning = errors.New("vm is not running")

// VmBackend holds the OS-specific VM operations. Implementations must not leak
// backend-specific concepts (WSL distro names, VHDX paths, Lima instance
// names) through this interface.
type VmBackend interface {
	// Start boots the VM and returns once the container engine responds.
	Start() error
	Stop() error
	Exec(args []string) (ExecResult, error)
	Status() (VmStatus, error)
	SetResources(resources Resources) error
}
