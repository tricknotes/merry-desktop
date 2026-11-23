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

// Resources is what the VM may use. Zero means "backend default".
type Resources struct {
	CPUs      int `json:"cpus"`
	MemoryMiB int `json:"memoryMiB"`
	// DiskGiB is the upper limit of the VM disk.
	DiskGiB int `json:"diskGiB"`
}

// ResourcePlan says what applying new resources involves, so that the UI can
// warn before saving.
type ResourcePlan struct {
	// RestartVM: the VM must restart for the change to take effect.
	RestartVM bool `json:"restartVM"`
	// RestartHost: the change applies to every VM of this kind on the machine
	// (for WSL2: all distros of the user), and all of them must restart.
	RestartHost bool `json:"restartHost"`
}

// ResourceManager is implemented by backends whose resource settings can be
// read back and whose changes need a restart.
type ResourceManager interface {
	// CurrentResources returns the configured values (zero = backend default).
	CurrentResources(ctx context.Context) (Resources, error)
	// PlanResources describes what SetResources(r) would involve.
	PlanResources(ctx context.Context, r Resources) (ResourcePlan, error)
	// RestartHost restarts every VM of this kind (see ResourcePlan.RestartHost)
	// and starts this VM again if it was running.
	RestartHost(ctx context.Context) error
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
	// ErrHostNotReady wraps errors from Provisioner.CheckHost. The wrapping
	// error's message tells the user what to do.
	ErrHostNotReady = errors.New("this machine is not ready to run the VM")
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

// MinDiskGiB is the smallest disk limit accepted.
const MinDiskGiB = 16

// Validate checks values that every backend requires. Zero is always allowed
// and means "backend default".
func (r Resources) Validate() error {
	if r.CPUs < 0 {
		return errors.New("cpus must be 0 (default) or >= 1")
	}
	if r.MemoryMiB != 0 && r.MemoryMiB < 1024 {
		return errors.New("memoryMiB must be 0 (default) or >= 1024")
	}
	if r.DiskGiB != 0 && r.DiskGiB < MinDiskGiB {
		return errors.New("diskGiB must be 0 (default) or >= 16")
	}
	return nil
}

// Network is how the VM reaches the outside (F-11).
type Network struct {
	// ProxyURL is used for HTTP and HTTPS. It may contain credentials and must
	// never be logged; use Redacted for display.
	ProxyURL string `json:"-"`
	NoProxy  string `json:"noProxy"`
	// CACertsPEM are extra trusted certificates (PEM), such as a corporate CA.
	CACertsPEM []byte `json:"-"`
	// Note explains something the user should know (for example: a PAC file
	// is configured but not evaluated).
	Note string `json:"note"`
}

// SetupStep identifies a step of the setup wizard (F-10).
type SetupStep string

const (
	StepCheckHost SetupStep = "check"
	StepNetwork   SetupStep = "network"
	StepCreate    SetupStep = "create"
	StepStart     SetupStep = "start"
	StepVerify    SetupStep = "verify"
	StepHostCLI   SetupStep = "cli"
)

// Provisioner is implemented by backends that can create their VM.
type Provisioner interface {
	// CheckHost reports whether this machine can run the VM. The error explains
	// what the user has to do (it may need an administrator).
	CheckHost(ctx context.Context) error
	// DetectNetwork reads the proxy and CA settings of the host.
	DetectNetwork(ctx context.Context) (Network, error)
	// Provisioned reports whether the VM exists.
	Provisioned(ctx context.Context) (bool, error)
	// Provision creates the VM with the container engine. progress receives
	// short, user-facing lines; it must not block.
	Provision(ctx context.Context, network Network, progress func(string)) error
}

// Maintainer is implemented by backends that need periodic upkeep while the VM
// runs (for example re-creating mounts lost when another VM restarted).
type Maintainer interface {
	Maintain(ctx context.Context) error
}

// HostIntegrator is implemented by backends that set up the docker CLI on the
// host (F-04): installing it, adding it to PATH, creating a context if needed.
type HostIntegrator interface {
	IntegrateHost(ctx context.Context, network Network, progress func(string)) error
}

// RegistryLogin is implemented by backends that can sign the host CLI in to a
// registry, keeping the credentials in the OS credential store (F-12).
type RegistryLogin interface {
	Login(ctx context.Context, server, username, password string) error
}
