import path from "node:path";
import { app, BrowserWindow, ipcMain, Menu, nativeImage, Tray } from "electron";
import { BENCH_REVISION, VmController } from "./core";
import { StubBackend } from "./stub-backend";
import type { Resources, VmStatus } from "./vm-backend";

const STATUS_LABELS: Record<VmStatus, string> = {
  stopped: "停止中",
  starting: "起動中",
  running: "稼働中",
  error: "異常",
};

const assetsDir = path.join(__dirname, "../../assets");
const controller = new VmController(new StubBackend());

let tray: Tray | null = null;
let settingsWindow: BrowserWindow | null = null;

function trayIcon(status: VmStatus): Electron.NativeImage {
  return nativeImage.createFromPath(path.join(assetsDir, `tray-${status}.png`));
}

function buildMenu(status: VmStatus): Menu {
  const busy = status === "starting";
  return Menu.buildFromTemplate([
    { label: `状態: ${STATUS_LABELS[status]}`, enabled: false },
    { type: "separator" },
    { label: "起動", enabled: status === "stopped" || status === "error", click: () => void controller.start() },
    { label: "停止", enabled: !busy && status !== "stopped", click: () => void controller.stop() },
    { type: "separator" },
    { label: "設定を開く", click: openSettings },
    { type: "separator" },
    { label: "終了", click: () => app.quit() },
  ]);
}

function refreshTray(status: VmStatus): void {
  if (!tray) return;
  tray.setImage(trayIcon(status));
  tray.setToolTip(`Spike (Electron): ${STATUS_LABELS[status]}`);
  tray.setContextMenu(buildMenu(status));
}

function openSettings(): void {
  if (settingsWindow) {
    settingsWindow.show();
    settingsWindow.focus();
    return;
  }
  settingsWindow = new BrowserWindow({
    width: 360,
    height: 320,
    title: "設定",
    autoHideMenuBar: true,
    webPreferences: {
      preload: path.join(__dirname, "../preload/preload.js"),
      contextIsolation: true,
      sandbox: true,
      nodeIntegration: false,
    },
  });
  settingsWindow.on("closed", () => {
    settingsWindow = null;
  });
  void settingsWindow.loadFile(path.join(__dirname, "../renderer/index.html"));
}

ipcMain.handle("vm:get-snapshot", () => controller.snapshot());
ipcMain.handle("vm:set-resources", (_event, resources: Resources) => controller.setResources(resources));

controller.on("status", (status) => {
  refreshTray(status);
  settingsWindow?.webContents.send("vm:status", status);
});

// Keep running in the tray after the last window is closed.
app.on("window-all-closed", () => {});

void app.whenReady().then(() => {
  console.info(`bench revision ${BENCH_REVISION}`);
  tray = new Tray(trayIcon(controller.status()));
  refreshTray(controller.status());
});
