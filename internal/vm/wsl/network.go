package wsl

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/tricknotes/merry-desktop/internal/vm"
)

var _ vm.NetworkApplier = (*Backend)(nil)

// networkStamp is where the VM keeps the fingerprint of the applied settings.
const networkStamp = "/etc/engine/network.sha256"

const networkDir = "/tmp/engine-network"

func networkFingerprint(n vm.Network) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\n%s\n", n.ProxyURL, n.NoProxy)
	h.Write(n.CACertsPEM)
	return hex.EncodeToString(h.Sum(nil))
}

// ApplyNetwork re-runs the network part of the provisioning
// (engine-apply-network) when the host settings changed since they were last
// applied. A VM provisioned before the fingerprint existed gets the settings
// applied and the fingerprint written, and counts as unchanged: the values it
// has came from the same detection.
func (b *Backend) ApplyNetwork(ctx context.Context, n vm.Network) (bool, error) {
	sum := networkFingerprint(n)
	b.mu.Lock()
	known := b.appliedNetwork == sum
	b.mu.Unlock()
	if known {
		return false, nil
	}
	res, err := b.Exec(ctx, []string{"cat", networkStamp})
	if err != nil {
		return false, err
	}
	stamp := strings.TrimSpace(res.Stdout)
	if res.ExitCode == 0 && stamp == sum {
		b.remember(sum)
		return false, nil
	}
	bundle, _, err := setupBundle(n.CACertsPEM)
	if err != nil {
		return false, err
	}
	script := `set -e; rm -rf "$1"; mkdir -p "$1"; tar -x -C "$1"
ENGINE_CA_DIR="$1/ca" /usr/local/sbin/engine-apply-network >/dev/null
printf '%s\n' "$2" > ` + networkStamp + `
rm -rf "$1"`
	env := map[string]string{"ENGINE_PROXY": n.ProxyURL, "ENGINE_NO_PROXY": n.NoProxy}
	if err := b.mustRun(ctx, RunOptions{Stdin: bundle, Env: env},
		b.inDistro("sh", "-c", script, "sh", networkDir, sum)...); err != nil {
		return false, scrubErr(fmt.Errorf("apply the network settings: %w", err), n.ProxyURL)
	}
	b.remember(sum)
	return res.ExitCode == 0 && stamp != "", nil
}

func (b *Backend) remember(sum string) {
	b.mu.Lock()
	b.appliedNetwork = sum
	b.mu.Unlock()
}

// vmCABundle is the distro's CA bundle; apply-network.sh adds the host's
// certificates to it.
const vmCABundle = "/etc/ssl/certs/ca-certificates.crt"

var _ vm.ContainerCAConfigurer = (*Backend)(nil)

// SetContainerCA turns the CA bundle for new containers on or off. While the
// VM runs, only the agent session is reconnected; containers keep running.
func (b *Backend) SetContainerCA(ctx context.Context, enabled bool) error {
	b.mu.Lock()
	same := b.ContainerCA == enabled
	b.ContainerCA = enabled
	running := b.keepaliveRunning()
	b.mu.Unlock()
	if same || !running {
		return nil
	}
	if err := b.endSession(); err != nil {
		return err
	}
	return b.Start(ctx)
}
