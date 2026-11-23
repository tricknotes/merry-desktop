// Package appservice is what the settings and setup windows call. The app binds
// it to the Wails windows; the development CLI serves it over HTTP (devui).
package appservice

import (
	"context"
	"errors"
	"slices"

	"github.com/tricknotes/merry-desktop/internal/core"
	"github.com/tricknotes/merry-desktop/internal/settings"
	"github.com/tricknotes/merry-desktop/internal/vm"
)

// Event names sent to the windows.
const (
	EventChanged = "vm:changed"  // data: core.Snapshot
	EventSetup   = "setup:event" // data: core.SetupEvent
)

// Service exposes the controller and the settings to the windows. Every
// exported method is callable from the frontend.
type Service struct {
	Controller *core.Controller
	Settings   *settings.Store
	// Emit sends an event to the windows. It must not block.
	Emit func(name string, data any)
}

// New returns a Service and forwards status changes to emit as EventChanged.
func New(controller *core.Controller, store *settings.Store, emit func(name string, data any)) *Service {
	s := &Service{Controller: controller, Settings: store, Emit: emit}
	controller.OnChange(func(snap core.Snapshot) { s.emit(EventChanged, snap) })
	return s
}

func (s *Service) emit(name string, data any) {
	if s.Emit != nil {
		s.Emit(name, data)
	}
}

func (s *Service) GetSnapshot() core.Snapshot {
	return s.Controller.Snapshot()
}

// Start, Stop and Restart operate the VM (F-01). They return when done; the
// status changes are also sent as EventChanged.
func (s *Service) Start()   { s.Controller.Start(context.Background()) }
func (s *Service) Stop()    { s.Controller.Stop(context.Background()) }
func (s *Service) Restart() { s.Controller.Restart(context.Background()) }

// RunSetup runs the setup wizard (F-10) and reports progress as EventSetup.
func (s *Service) RunSetup() error {
	return s.Controller.Setup(context.Background(), func(e core.SetupEvent) {
		s.emit(EventSetup, e)
	})
}

// PlanResources tells the settings window what saving would involve (F-03).
func (s *Service) PlanResources(resources vm.Resources) (vm.ResourcePlan, error) {
	return s.Controller.PlanResources(context.Background(), resources)
}

// ApplyResources saves the resources and, with restartNow, restarts what is needed.
func (s *Service) ApplyResources(resources vm.Resources, restartNow bool) error {
	err := s.Controller.ApplyResources(context.Background(), resources, restartNow)
	if errors.Is(err, vm.ErrNotSupported) {
		return errors.New("この環境では、まだ変更できません")
	}
	return err
}

// Integrations lists the WSL distros (or other environments) that can use the
// engine, and which are enabled (F-04).
func (s *Service) Integrations() ([]vm.Integration, error) {
	return s.Controller.Integrations(context.Background())
}

// SetIntegrations saves and applies the choice. nil means "the default".
func (s *Service) SetIntegrations(names []string) error {
	if err := s.Controller.SetIntegrations(context.Background(), names); err != nil {
		return err
	}
	_, err := s.Settings.Update(func(v *settings.Settings) {
		if names == nil {
			v.Integrations = nil
			return
		}
		v.Integrations = &names
	})
	return err
}

// Registries lists the registries offered for login (F-12).
func (s *Service) Registries() ([]string, error) {
	cur, err := s.Settings.Load()
	return cur.Registries, err
}

// AddRegistry remembers a registry (host[:port]) and returns the new list.
func (s *Service) AddRegistry(server string) ([]string, error) {
	server, err := core.NormalizeRegistry(server)
	if err != nil {
		return nil, err
	}
	cur, err := s.Settings.Update(func(v *settings.Settings) {
		if !slices.Contains(v.Registries, server) {
			v.Registries = append(v.Registries, server)
		}
	})
	return cur.Registries, err
}

// RemoveRegistry forgets a registry. Its stored login is not deleted.
func (s *Service) RemoveRegistry(server string) ([]string, error) {
	cur, err := s.Settings.Update(func(v *settings.Settings) {
		v.Registries = slices.DeleteFunc(v.Registries, func(r string) bool { return r == server })
	})
	return cur.Registries, err
}

// Login signs the host docker CLI in to a registry. The password is passed
// through to `docker login --password-stdin` and not kept by the app.
func (s *Service) Login(server, username, password string) error {
	return s.Controller.Login(context.Background(), server, username, password)
}
