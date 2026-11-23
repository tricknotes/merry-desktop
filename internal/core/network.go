package core

import (
	"context"
	"strings"

	"github.com/tricknotes/merry-desktop/internal/vm"
)

// NetworkChange is the outcome of SyncNetwork.
type NetworkChange struct {
	Changed   bool
	Network   vm.Network
	Restarted bool // the VM was restarted to use the new settings
}

// SyncNetwork follows changes of the host proxy and CA settings, such as
// connecting to or leaving a VPN (C-2). It applies changed settings to the VM.
// The engine reads them when it starts, so the VM is restarted right away when
// no container runs; otherwise the user is told to restart it, to not stop
// their containers. Call it periodically.
func (c *Controller) SyncNetwork(ctx context.Context) (NetworkChange, error) {
	p, ok := c.backend.(vm.Provisioner)
	a, ok2 := c.backend.(vm.NetworkApplier)
	if !ok || !ok2 || c.Snapshot().Status != vm.StatusRunning {
		return NetworkChange{}, nil
	}
	n, err := p.DetectNetwork(ctx)
	if err != nil {
		return NetworkChange{}, err
	}
	changed, err := a.ApplyNetwork(ctx, n)
	if err != nil || !changed {
		return NetworkChange{Network: n}, err
	}
	change := NetworkChange{Changed: true, Network: n}
	res, err := c.backend.Exec(ctx, []string{"docker", "ps", "--quiet"})
	running := err != nil || res.ExitCode != 0 || strings.TrimSpace(res.Stdout) != ""
	if running {
		c.notice("プロキシか証明書の設定が変わったので、VM に反映しました。動いているコンテナがあるため再起動していません。" +
			"docker pull や新しいコンテナで新しい設定を使うには、トレイから再起動してください。")
		return change, nil
	}
	c.log.Info("network settings changed; restarting the VM")
	c.Restart(ctx)
	change.Restarted = true
	c.notice("プロキシか証明書の設定が変わったので、VM に反映して再起動しました。")
	return change, nil
}
