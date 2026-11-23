package wsl

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/tricknotes/merry-desktop/internal/vm"
)

// Integration with the user's other WSL distros (F-04): a docker CLI inside
// such a distro reaches the engine through its own socket, and the agent
// rewrites its paths to where the distro's root is bind-mounted.
//
// By default only the user's default WSL distro is integrated, to change as
// little as possible in distros the app does not own.

// otherEngines are distros of other container tools; they are never integrated.
var otherEngines = []string{"docker-desktop", "rancher-desktop", "podman-machine"}

func (b *Backend) runDir() string { return "/mnt/wsl/" + b.Distro + "/run" }

// distroSocket is the socket for a docker CLI inside the given distro.
func (b *Backend) distroSocket(distro string) string {
	return b.runDir() + "/" + distro + "/docker.sock"
}

// integrationTargets picks the distros to integrate from `wsl -l -v`.
func (b *Backend) integrationTargets(distros []distro) []string {
	if b.Distros != nil {
		return b.Distros
	}
	for _, d := range distros {
		if !d.Default || d.Version != "2" || strings.EqualFold(d.Name, b.Distro) || isOtherEngine(d.Name) {
			continue
		}
		return []string{d.Name}
	}
	return nil
}

func isOtherEngine(name string) bool {
	for _, prefix := range otherEngines {
		if strings.HasPrefix(strings.ToLower(name), prefix) {
			return true
		}
	}
	return false
}

// Maintain re-creates what is lost when an integrated distro restarts: the
// bind mount of its root, and makes sure its docker context exists. Distros
// that are not running are left alone (they are not started).
func (b *Backend) Maintain(ctx context.Context) error {
	b.mu.Lock()
	targets := append([]string(nil), b.integrated...)
	b.mu.Unlock()
	if len(targets) == 0 {
		return nil
	}
	res, err := b.Runner.Run(ctx, "--list", "--verbose")
	if err != nil {
		return err
	}
	list := parseList(res.Stdout)
	var errs []error
	for _, name := range targets {
		d, ok := findDistro(list, name)
		if !ok || d.State != stateRunning {
			continue
		}
		if err := b.integrate(ctx, name); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
		}
	}
	return errors.Join(errs...)
}

const mountScript = `d="$1"; mkdir -p "$d" && { mountpoint -q "$d" || mount --bind / "$d"; }`

// contextScript creates the docker context in the distro if it has a docker
// CLI. It switches to it only when the user still uses the default context and
// no other engine socket exists, so an existing setup is never taken over.
const contextScript = `# wsl --exec runs without a login shell, so add where docker is usually installed.
PATH="$PATH:/usr/local/bin:/home/linuxbrew/.linuxbrew/bin:$HOME/.linuxbrew/bin:$HOME/.local/bin:$HOME/bin:/snap/bin"
command -v docker >/dev/null 2>&1 || exit 0
name="$1"; sock="$2"
docker context inspect "$name" >/dev/null 2>&1 ||
  docker context create "$name" --description "container engine VM" --docker "host=unix://$sock" >/dev/null || exit 1
if [ "$(docker context show 2>/dev/null)" = default ] && [ ! -S /var/run/docker.sock ]; then
  docker context use "$name" >/dev/null
fi`

func (b *Backend) integrate(ctx context.Context, distro string) error {
	mountpoint := b.distroRoot() + "/" + distro
	if err := b.mustRun(ctx, RunOptions{},
		"--distribution", distro, "--user", "root", "--exec", "sh", "-c", mountScript, "sh", mountpoint); err != nil {
		return fmt.Errorf("bind-mount the distro root: %w", err)
	}
	b.mu.Lock()
	done := b.contextDone[distro]
	b.mu.Unlock()
	if done {
		return nil
	}
	// As the distro's default user: the docker config lives in their home.
	if err := b.mustRun(ctx, RunOptions{},
		"--distribution", distro, "--exec", "sh", "-c", contextScript, "sh", b.Distro, b.distroSocket(distro)); err != nil {
		return fmt.Errorf("docker context: %w", err)
	}
	b.mu.Lock()
	if b.contextDone == nil {
		b.contextDone = map[string]bool{}
	}
	b.contextDone[distro] = true
	b.mu.Unlock()
	return nil
}

var _ vm.Integrator = (*Backend)(nil)

// Integrations lists the user's WSL2 distros that can be integrated.
func (b *Backend) Integrations(ctx context.Context) ([]vm.Integration, error) {
	res, err := b.Runner.Run(ctx, "--list", "--verbose")
	if err != nil {
		return nil, err
	}
	list := parseList(res.Stdout)
	b.mu.Lock()
	enabled := b.integrationTargets(list)
	b.mu.Unlock()
	var out []vm.Integration
	for _, d := range list {
		if d.Version != "2" || strings.EqualFold(d.Name, b.Distro) || isOtherEngine(d.Name) {
			continue
		}
		out = append(out, vm.Integration{Name: d.Name, Default: d.Default, Enabled: slices.Contains(enabled, d.Name)})
	}
	return out, nil
}

// SetIntegrations changes the integrated distros. While the VM runs, only the
// agent session is reconnected: the distro (and its containers) keeps running.
// Distros that are no longer integrated get their root bind mount removed; the
// docker context in them is left as is.
func (b *Backend) SetIntegrations(ctx context.Context, names []string) error {
	b.mu.Lock()
	b.Distros = names
	running := b.keepaliveRunning()
	before := append([]string(nil), b.integrated...)
	b.mu.Unlock()
	if !running {
		return nil
	}
	if err := b.endSession(); err != nil {
		return err
	}
	if err := b.Start(ctx); err != nil {
		return err
	}
	b.mu.Lock()
	after := b.integrated
	b.mu.Unlock()
	for _, d := range before {
		if !slices.Contains(after, d) {
			_ = b.mustRun(ctx, RunOptions{}, "--distribution", d, "--user", "root", "--exec",
				"sh", "-c", `mountpoint -q "$1" && umount "$1"; true`, "sh", b.distroRoot()+"/"+d)
		}
	}
	return nil
}
