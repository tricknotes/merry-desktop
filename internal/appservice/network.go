package appservice

import (
	"context"
	"log/slog"

	"github.com/tricknotes/merry-desktop/internal/core"
	"github.com/tricknotes/merry-desktop/internal/settings"
)

// SyncNetwork follows changes of the host proxy and CA settings (C-2): the VM
// gets them, and when the build proxy (C-4) is on, the docker CLI configs get
// the new proxy too. Without a usable proxy (none, or one with credentials)
// the configs are left as they are. Call it periodically.
func SyncNetwork(ctx context.Context, s *Service, log *slog.Logger) {
	change, err := s.Controller.SyncNetwork(ctx)
	if err != nil {
		log.Warn("sync network", "err", err)
		return
	}
	if !change.Changed {
		return
	}
	cur, err := s.Settings.Load()
	if err != nil || cur.BuildProxy == nil {
		return
	}
	next, err := core.ClientProxyFor(change.Network)
	if err != nil || next == *cur.BuildProxy {
		return
	}
	if _, err := s.Controller.DisableBuildProxy(ctx, *cur.BuildProxy); err != nil {
		log.Warn("build proxy: remove the old proxy", "err", err)
		return
	}
	written, _, err := s.Controller.EnableBuildProxy(ctx)
	if err != nil {
		log.Warn("build proxy: write the new proxy", "err", err)
		return
	}
	if _, err := s.Settings.Update(func(v *settings.Settings) { v.BuildProxy = &written }); err != nil {
		log.Warn("build proxy: save", "err", err)
	}
}
