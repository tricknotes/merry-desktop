import { contextBridge, ipcRenderer } from "electron";

contextBridge.exposeInMainWorld("spike", {
  getSnapshot: () => ipcRenderer.invoke("vm:get-snapshot"),
  setResources: (resources: unknown) => ipcRenderer.invoke("vm:set-resources", resources),
  onStatus: (listener: (status: string) => void) => {
    ipcRenderer.on("vm:status", (_event, status: string) => listener(status));
  },
});
