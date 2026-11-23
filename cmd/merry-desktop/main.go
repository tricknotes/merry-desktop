// Command merry-desktop is the tray app that runs the container engine VM.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/tricknotes/merry-desktop/frontend"
	"github.com/tricknotes/merry-desktop/internal/appinfo"
	"github.com/tricknotes/merry-desktop/internal/applog"
	"github.com/tricknotes/merry-desktop/internal/appservice"
	"github.com/tricknotes/merry-desktop/internal/core"
	"github.com/tricknotes/merry-desktop/internal/settings"
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

func main() {
	launchedAtLogin := flag.Bool(autostartArg[2:], false, "launched at user login; also start the VM")
	flag.Parse()

	configDir, err := os.UserConfigDir()
	if err != nil {
		log.Fatal(err)
	}
	cacheDir, err := os.UserCacheDir() // %LOCALAPPDATA% on Windows
	if err != nil {
		log.Fatal(err)
	}
	logDir := filepath.Join(cacheDir, appinfo.ID, "logs")
	logger, logFile, err := applog.Open(logDir)
	if err != nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
		logger.Warn("log file", "err", err)
	} else {
		defer logFile.Close()
	}
	controller := core.NewController(newBackend(), logger)
	store := &settings.Store{Path: filepath.Join(configDir, appinfo.ID, "settings.json")}
	var app *application.App
	service := appservice.New(controller, store, func(name string, data any) {
		if app != nil {
			app.Event.Emit(name, data)
		}
	})

	app = application.New(application.Options{
		Name:     appinfo.DisplayName,
		Services: []application.Service{application.NewService(service)},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(frontend.Dist),
		},
		// Stay in the tray when the settings window closes; only Quit exits.
		Windows: application.WindowsOptions{DisableQuitOnLastWindowClosed: true},
		Linux:   application.LinuxOptions{DisableQuitOnLastWindowClosed: true},
	})

	openSettings := singleWindow(app, application.WebviewWindowOptions{
		Name: "settings", Title: appinfo.DisplayName + " - 設定", Width: 420, Height: 720, URL: "/",
	})
	openSetup := singleWindow(app, application.WebviewWindowOptions{
		Name: "setup", Title: appinfo.DisplayName + " - セットアップ", Width: 480, Height: 560, URL: "/setup.html",
	})

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
	menu.Add("セットアップ...").OnClick(func(*application.Context) { openSetup() })
	menu.Add("設定を開く").OnClick(func(*application.Context) { openSettings() })
	menu.Add("診断レポートを作成").OnClick(func(*application.Context) {
		go writeDiagnostics(app, controller, service, logDir, logger)
	})
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
	controller.OnChange(refreshTray)

	if enabled, err := app.Autostart.IsEnabled(); err == nil {
		autostartItem.SetChecked(enabled)
		menu.Update()
	} else {
		logger.Warn("autostart status", "err", err)
	}

	// Start the background work once the app is up, so that windows can be opened.
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		go backgroundLoop(controller, store, logger, openSetup, *launchedAtLogin)
	})

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

// singleWindow returns a function that shows the window, creating it on first
// use and again after it was closed (windows are destroyed on close).
func singleWindow(app *application.App, opts application.WebviewWindowOptions) func() {
	var (
		mu     sync.Mutex
		window *application.WebviewWindow
	)
	return func() {
		mu.Lock()
		defer mu.Unlock()
		if window != nil {
			window.Show()
			window.Focus()
			return
		}
		w := app.Window.NewWithOptions(opts)
		w.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
			mu.Lock()
			defer mu.Unlock()
			window = nil
		})
		window = w
	}
}

// backgroundLoop loads the settings, guides a first run to the setup window,
// starts the VM after login (F-05) and keeps the tray status fresh.
func backgroundLoop(controller *core.Controller, store *settings.Store, logger *slog.Logger, openSetup func(), launchedAtLogin bool) {
	ctx := context.Background()
	if saved, err := store.Load(); err != nil {
		logger.Warn("load settings", "err", err)
	} else if err := controller.ApplySavedIntegrations(ctx, saved.Integrations); err != nil {
		logger.Warn("apply integrations", "err", err)
	}
	if err := controller.LoadResources(ctx); err != nil {
		logger.Warn("load resources", "err", err)
	}
	needSetup, err := controller.NeedsSetup(ctx)
	if err != nil {
		logger.Warn("check setup", "err", err)
	}
	if needSetup {
		// First run: guide the user instead of only showing an error icon.
		openSetup()
	}
	controller.Refresh(ctx)
	if launchedAtLogin && !needSetup && controller.Snapshot().Status == vm.StatusStopped {
		controller.Start(ctx)
	}
	ticker := time.NewTicker(refreshInterval)
	defer ticker.Stop()
	for range ticker.C {
		controller.Refresh(ctx)
	}
}

// writeDiagnostics writes the diagnostic report (F-14) to the Downloads folder
// and shows it in the file manager.
func writeDiagnostics(app *application.App, controller *core.Controller, service *appservice.Service, logDir string, logger *slog.Logger) {
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, "Downloads", fmt.Sprintf("%s-diagnostics-%s.zip", appinfo.ID, time.Now().Format("20060102-150405")))
	registries, _ := service.Registries()
	err := controller.WriteDiagnostics(context.Background(), path, core.DiagnosticsInput{
		AppLogDir: logDir, SettingsPath: service.Settings.Path, Registries: registries,
	})
	if err != nil {
		logger.Error("diagnostics", "err", err)
		app.Dialog.Error().SetTitle("診断レポート").SetMessage("作成できませんでした: " + err.Error()).Show()
		return
	}
	logger.Info("diagnostics written", "path", path)
	if err := app.Env.OpenFileManager(path, true); err != nil {
		app.Dialog.Info().SetTitle("診断レポート").SetMessage("作成しました: " + path).Show()
	}
}
