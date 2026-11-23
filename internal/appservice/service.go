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
	EventNotice  = "vm:notice"   // data: string
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
	controller.OnNotice(func(msg string) { s.emit(EventNotice, msg) })
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

// RecreateVM deletes the VM with its images, containers and volumes, then runs
// the setup again (progress as EventSetup). The window confirms first.
func (s *Service) RecreateVM() error {
	return s.Controller.Recreate(context.Background(), func(e core.SetupEvent) {
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

// ProjectUsage reads the CPU and memory use of the running containers per
// Compose project (I-07).
func (s *Service) ProjectUsage() (core.Usage, error) {
	u, err := s.Controller.ProjectUsage(context.Background())
	return u, notRunningError(err)
}

// LocalImages lists the images in the engine, to pick one to scan (I-02).
func (s *Service) LocalImages() ([]string, error) {
	images, err := s.Controller.LocalImages(context.Background())
	return images, notRunningError(err)
}

// ScanImage scans a local image for known vulnerabilities with Trivy (I-02).
func (s *Service) ScanImage(image string) (core.ScanResult, error) {
	r, err := s.Controller.ScanImage(context.Background(), image)
	return r, notRunningError(err)
}

// AutoCleanupEnabled reports whether old build cache and untagged images are
// removed daily (I-22).
func (s *Service) AutoCleanupEnabled() (bool, error) {
	cur, err := s.Settings.Load()
	return cur.AutoCleanupEnabled(), err
}

// SetAutoCleanup turns the daily cleanup on or off.
func (s *Service) SetAutoCleanup(enable bool) error {
	_, err := s.Settings.Update(func(v *settings.Settings) { v.AutoCleanup = &enable })
	return err
}

// CleanupCandidates lists what could be removed to free space in the VM.
func (s *Service) CleanupCandidates() (core.CleanupReport, error) {
	r, err := s.Controller.CleanupCandidates(context.Background())
	return r, notRunningError(err)
}

// RemoveUnused removes the containers and images the user picked.
func (s *Service) RemoveUnused(containers, images []string) error {
	return notRunningError(s.Controller.RemoveUnused(context.Background(), containers, images))
}

func notRunningError(err error) error {
	if errors.Is(err, vm.ErrNotRunning) {
		return errors.New("VM が動いていません")
	}
	return err
}

// BuildProxyEnabled reports whether the proxy is written into the docker CLI
// configs for builds (C-4).
func (s *Service) BuildProxyEnabled() (bool, error) {
	cur, err := s.Settings.Load()
	return cur.BuildProxy != nil, err
}

// SetBuildProxy writes or removes the proxy in the docker CLI configs of the
// host and the integrated distros, and returns one line per place.
func (s *Service) SetBuildProxy(enable bool) ([]string, error) {
	ctx := context.Background()
	cur, err := s.Settings.Load()
	if err != nil {
		return nil, err
	}
	if !enable {
		if cur.BuildProxy == nil {
			return nil, nil
		}
		lines, err := s.Controller.DisableBuildProxy(ctx, *cur.BuildProxy)
		if err != nil {
			return lines, err
		}
		_, err = s.Settings.Update(func(v *settings.Settings) { v.BuildProxy = nil })
		return lines, err
	}
	written, lines, err := s.Controller.EnableBuildProxy(ctx)
	if err != nil {
		return lines, err
	}
	_, err = s.Settings.Update(func(v *settings.Settings) { v.BuildProxy = &written })
	return lines, err
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
