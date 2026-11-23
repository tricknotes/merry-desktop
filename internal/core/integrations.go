package core

import (
	"context"

	"github.com/tricknotes/merry-desktop/internal/vm"
)

// Integrations lists where else the engine can be used from (F-04).
func (c *Controller) Integrations(ctx context.Context) ([]vm.Integration, error) {
	i, ok := c.backend.(vm.Integrator)
	if !ok {
		return nil, nil
	}
	return i.Integrations(ctx)
}

// SetIntegrations chooses them; nil means the backend's default. A running VM
// keeps running.
func (c *Controller) SetIntegrations(ctx context.Context, names []string) error {
	i, ok := c.backend.(vm.Integrator)
	if !ok {
		return vm.ErrNotSupported
	}
	if err := i.SetIntegrations(ctx, names); err != nil {
		c.log.Error("set integrations", "err", err)
		return err
	}
	return nil
}

// ApplySavedIntegrations applies the choice stored in the settings, if any.
func (c *Controller) ApplySavedIntegrations(ctx context.Context, saved *[]string) error {
	if saved == nil {
		return nil
	}
	return c.SetIntegrations(ctx, *saved)
}
