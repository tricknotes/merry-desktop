// Command merry-desktop is the tray app that runs the container engine VM.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/tricknotes/merry-desktop/frontend"
	"github.com/tricknotes/merry-desktop/internal/appinfo"
	"github.com/tricknotes/merry-desktop/internal/core"
	"github.com/tricknotes/merry-desktop/internal/ui/icons"
	"github.com/tricknotes/merry-desktop/internal/vm"
)

// refreshInterval is how often the tray re-reads the VM status, to notice
// changes made outside the app (for example `wsl --shutdown`).
const refreshInterval = 10 * time.Second

var statusLabels = map[vm.Status]string{
	vm.StatusStopped:  "停止中",
	vm.StatusStarting: "起動中",
	vm.StatusRunning:  "稼働中",
	vm.StatusError:    "異常",
}

// engineName is the VM to drive. MERRY_ENGINE_NAME overrides it, which lets a
// VM made by scripts/phase0/wsl2 (phase0-engine) be used before F-10 exists.
func engineName() string {
	if name := os.Getenv("MERRY_ENGINE_NAME"); name != "" {
		return name
	}
	return appinfo.EngineName
}

// autostartArg marks a launch at user login (F-05). Such a launch also starts
// the VM, so that `docker` works right after login without opening the tray.
const autostartArg = "--autostart"

// VMService is bound to the settings window.
type VMService struct {
	controller *core.Controller
}

func (s *VMService) GetSnapshot() core.Snapshot {
	return s.controller.Snapshot()
}

func (s *VMService) SetResources(resources vm.Resources) error {
	err := s.controller.SetResources(context.Background(), resources)
	if errors.Is(err, vm.ErrNotSupported) {
		return errors.New("この環境では、まだ変更できません")
	}
	return err
}

func main() {
	launchedAtLogin := flag.Bool(autostartArg[2:], false, "launched at user login; also start the VM")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	controller := core.NewController(newBackend(), logger)

	app := application.New(application.Options{
		Name:     appinfo.DisplayName,
		Services: []application.Service{application.NewService(&VMService{controller: controller})},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(frontend.Dist),
		},
		// Stay in the tray when the settings window closes; only Quit exits.
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
			Title:  appinfo.DisplayName + " - 設定",
			Width:  380,
			Height: 360,
			URL:    "/",
		})
		window.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
			settingsMu.Lock()
			defer settingsMu.Unlock()
			settings = nil
		})
		settings = window
	}

	// VM operations block (Start waits for the engine), so run them off the UI thread.
	run := func(op func(context.Context)) {
		go op(context.Background())
	}

	tray := app.SystemTray.New()
	menu := app.NewMenu()
	statusItem := menu.Add("").SetEnabled(false)
	menu.AddSeparator()
	startItem := menu.Add("起動").OnClick(func(*application.Context) { run(controller.Start) })
	stopItem := menu.Add("停止").OnClick(func(*application.Context) { run(controller.Stop) })
	restartItem := menu.Add("再起動").OnClick(func(*application.Context) { run(controller.Restart) })
	menu.AddSeparator()
	menu.Add("設定を開く").OnClick(func(*application.Context) { openSettings() })
	autostartItem := menu.AddCheckbox("ログイン時に起動", false)
	autostartItem.OnClick(func(ctx *application.Context) {
		item := ctx.ClickedMenuItem()
		if err := setAutostart(app, item.Checked()); err != nil {
			logger.Error("autostart", "err", err)
			item.SetChecked(!item.Checked())
			menu.Update()
		}
	})
	menu.AddSeparator()
	menu.Add("終了").OnClick(func(*application.Context) { app.Quit() })
	tray.SetMenu(menu)

	refreshTray := func(s core.Snapshot) {
		label := statusLabels[s.Status]
		tray.SetIcon(icons.Tray(s.Status))
		tooltip := appinfo.DisplayName + ": " + label
		if s.Message != "" {
			tooltip += "\n" + s.Message
		}
		tray.SetTooltip(tooltip)
		statusItem.SetLabel("状態: " + label)
		startItem.SetEnabled(s.Status == vm.StatusStopped || s.Status == vm.StatusError)
		stopItem.SetEnabled(s.Status == vm.StatusRunning || s.Status == vm.StatusError)
		restartItem.SetEnabled(s.Status == vm.StatusRunning || s.Status == vm.StatusError)
		menu.Update()
	}
	refreshTray(controller.Snapshot())
	controller.OnChange(func(s core.Snapshot) {
		refreshTray(s)
		app.Event.Emit("vm:changed", s)
	})

	if enabled, err := app.Autostart.IsEnabled(); err == nil {
		autostartItem.SetChecked(enabled)
		menu.Update()
	} else {
		logger.Warn("autostart status", "err", err)
	}

	go func() {
		controller.Refresh(context.Background())
		if *launchedAtLogin && controller.Snapshot().Status == vm.StatusStopped {
			controller.Start(context.Background())
		}
		ticker := time.NewTicker(refreshInterval)
		defer ticker.Stop()
		for range ticker.C {
			controller.Refresh(context.Background())
		}
	}()

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

// setAutostart registers or removes the login item (HKCU\...\Run on Windows).
func setAutostart(app *application.App, enabled bool) error {
	if !enabled {
		return app.Autostart.Disable()
	}
	return app.Autostart.EnableWithOptions(application.AutostartOptions{
		Identifier: appinfo.ID,
		Arguments:  []string{autostartArg},
	})
}
