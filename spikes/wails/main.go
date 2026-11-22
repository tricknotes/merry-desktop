package main

import (
	"embed"
	"log"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

//go:embed all:frontend/dist
var assets embed.FS

var (
	//go:embed icons/tray-stopped.png
	iconStopped []byte
	//go:embed icons/tray-starting.png
	iconStarting []byte
	//go:embed icons/tray-running.png
	iconRunning []byte
	//go:embed icons/tray-error.png
	iconError []byte
)

var statusLabels = map[VmStatus]string{
	StatusStopped:  "停止中",
	StatusStarting: "起動中",
	StatusRunning:  "稼働中",
	StatusError:    "異常",
}

var trayIcons = map[VmStatus][]byte{
	StatusStopped:  iconStopped,
	StatusStarting: iconStarting,
	StatusRunning:  iconRunning,
	StatusError:    iconError,
}

// VmService is bound to the frontend.
type VmService struct {
	controller *VmController
}

func (s *VmService) GetSnapshot() Snapshot {
	return s.controller.Snapshot()
}

func (s *VmService) SetResources(resources Resources) error {
	return s.controller.SetResources(resources)
}

func main() {
	log.Printf("bench revision %d", BenchRevision)
	controller := NewVmController(NewStubBackend())

	app := application.New(application.Options{
		Name:     "SpikeWails",
		Services: []application.Service{application.NewService(&VmService{controller: controller})},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		// Stay in the tray when the last window closes; only the Quit menu exits.
		Windows: application.WindowsOptions{DisableQuitOnLastWindowClosed: true},
		Linux:   application.LinuxOptions{DisableQuitOnLastWindowClosed: true},
	})

	var (
		settingsMu sync.Mutex
		settings   *application.WebviewWindow
	)
	openSettings := func() {
		settingsMu.Lock()
		defer settingsMu.Unlock()
		if settings != nil {
			settings.Show()
			settings.Focus()
			return
		}
		window := app.Window.NewWithOptions(application.WebviewWindowOptions{
			Name:   "settings",
			Title:  "設定",
			Width:  360,
			Height: 320,
			URL:    "/",
		})
		window.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
			settingsMu.Lock()
			defer settingsMu.Unlock()
			settings = nil
		})
		settings = window
	}

	tray := app.SystemTray.New()
	menu := app.NewMenu()
	statusItem := menu.Add("").SetEnabled(false)
	menu.AddSeparator()
	startItem := menu.Add("起動").OnClick(func(*application.Context) { go controller.Start() })
	stopItem := menu.Add("停止").OnClick(func(*application.Context) { go controller.Stop() })
	menu.AddSeparator()
	menu.Add("設定を開く").OnClick(func(*application.Context) { openSettings() })
	menu.AddSeparator()
	menu.Add("終了").OnClick(func(*application.Context) { app.Quit() })
	tray.SetMenu(menu)

	refreshTray := func(status VmStatus) {
		label := statusLabels[status]
		tray.SetIcon(trayIcons[status])
		tray.SetTooltip("Spike (Wails): " + label)
		statusItem.SetLabel("状態: " + label)
		startItem.SetEnabled(status == StatusStopped || status == StatusError)
		stopItem.SetEnabled(status == StatusRunning || status == StatusError)
		menu.Update()
	}
	refreshTray(StatusStopped)

	controller.OnStatus(func(status VmStatus) {
		refreshTray(status)
		app.Event.Emit("vm-status", string(status))
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
