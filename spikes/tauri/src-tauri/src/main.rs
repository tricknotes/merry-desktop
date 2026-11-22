// Hide the console window in release builds on Windows.
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

mod core;
mod stub;
mod vm;

use crate::core::{Snapshot, VmController, BENCH_REVISION};
use crate::stub::StubBackend;
use crate::vm::{Resources, VmStatus};
use std::sync::Arc;
use tauri::image::Image;
use tauri::menu::{Menu, MenuItem, PredefinedMenuItem};
use tauri::tray::TrayIconBuilder;
use tauri::{AppHandle, Emitter, Manager, RunEvent, State, WebviewUrl, WebviewWindowBuilder, Wry};

const TRAY_ID: &str = "main";
const SETTINGS_LABEL: &str = "settings";

type Controller = Arc<VmController>;

fn status_label(status: VmStatus) -> &'static str {
    match status {
        VmStatus::Stopped => "停止中",
        VmStatus::Starting => "起動中",
        VmStatus::Running => "稼働中",
        VmStatus::Error => "異常",
    }
}

fn tray_icon(status: VmStatus) -> Image<'static> {
    let bytes: &[u8] = match status {
        VmStatus::Stopped => include_bytes!("../icons/tray-stopped.png"),
        VmStatus::Starting => include_bytes!("../icons/tray-starting.png"),
        VmStatus::Running => include_bytes!("../icons/tray-running.png"),
        VmStatus::Error => include_bytes!("../icons/tray-error.png"),
    };
    Image::from_bytes(bytes).expect("bundled tray icon is a valid PNG")
}

fn build_menu(app: &AppHandle, status: VmStatus) -> tauri::Result<Menu<Wry>> {
    let can_start = matches!(status, VmStatus::Stopped | VmStatus::Error);
    let can_stop = matches!(status, VmStatus::Running | VmStatus::Error);
    let header = format!("状態: {}", status_label(status));
    Menu::with_items(
        app,
        &[
            &MenuItem::with_id(app, "status", header, false, None::<&str>)?,
            &PredefinedMenuItem::separator(app)?,
            &MenuItem::with_id(app, "start", "起動", can_start, None::<&str>)?,
            &MenuItem::with_id(app, "stop", "停止", can_stop, None::<&str>)?,
            &PredefinedMenuItem::separator(app)?,
            &MenuItem::with_id(app, "settings", "設定を開く", true, None::<&str>)?,
            &PredefinedMenuItem::separator(app)?,
            &MenuItem::with_id(app, "quit", "終了", true, None::<&str>)?,
        ],
    )
}

fn refresh_tray(app: &AppHandle, status: VmStatus) {
    let Some(tray) = app.tray_by_id(TRAY_ID) else { return };
    let _ = tray.set_icon(Some(tray_icon(status)));
    let _ = tray.set_tooltip(Some(format!("Spike (Tauri): {}", status_label(status))));
    match build_menu(app, status) {
        Ok(menu) => {
            let _ = tray.set_menu(Some(menu));
        }
        Err(err) => eprintln!("failed to build tray menu: {err}"),
    }
}

fn open_settings(app: &AppHandle) {
    if let Some(window) = app.get_webview_window(SETTINGS_LABEL) {
        let _ = window.show();
        let _ = window.set_focus();
        return;
    }
    // Creating a webview from a menu handler can deadlock on Windows, so build it off the main thread.
    let app = app.clone();
    std::thread::spawn(move || {
        let result = WebviewWindowBuilder::new(&app, SETTINGS_LABEL, WebviewUrl::App("index.html".into()))
            .title("設定")
            .inner_size(360.0, 320.0)
            .build();
        if let Err(err) = result {
            eprintln!("failed to open settings window: {err}");
        }
    });
}

fn run_in_background(app: &AppHandle, task: fn(&VmController)) {
    let controller = app.state::<Controller>().inner().clone();
    std::thread::spawn(move || task(&controller));
}

#[tauri::command]
fn get_snapshot(controller: State<'_, Controller>) -> Snapshot {
    controller.snapshot()
}

#[tauri::command]
fn set_resources(controller: State<'_, Controller>, resources: Resources) -> Result<(), String> {
    controller.set_resources(resources).map_err(|err| err.to_string())
}

fn main() {
    let app = tauri::Builder::default()
        .invoke_handler(tauri::generate_handler![get_snapshot, set_resources])
        .setup(|app| {
            println!("bench revision {BENCH_REVISION}");
            let handle = app.handle().clone();
            let controller: Controller = Arc::new(VmController::new(
                Box::new(StubBackend::new()),
                Box::new(move |status| {
                    let _ = handle.emit("vm-status", status);
                    let inner = handle.clone();
                    let _ = handle.run_on_main_thread(move || refresh_tray(&inner, status));
                }),
            ));
            app.manage(controller);

            let initial = VmStatus::Stopped;
            TrayIconBuilder::with_id(TRAY_ID)
                .icon(tray_icon(initial))
                .tooltip(format!("Spike (Tauri): {}", status_label(initial)))
                .menu(&build_menu(app.handle(), initial)?)
                .show_menu_on_left_click(true)
                .on_menu_event(|app, event| match event.id.as_ref() {
                    "start" => run_in_background(app, VmController::start),
                    "stop" => run_in_background(app, VmController::stop),
                    "settings" => open_settings(app),
                    "quit" => app.exit(0),
                    _ => {}
                })
                .build(app)?;
            Ok(())
        })
        .build(tauri::generate_context!())
        .expect("failed to build the Tauri application");

    app.run(|_app, event| {
        // Stay in the tray when the last window closes; only the Quit menu exits.
        if let RunEvent::ExitRequested { code: None, api, .. } = event {
            api.prevent_exit();
        }
    });
}
