import { invoke } from "@tauri-apps/api/core";
import { listen } from "@tauri-apps/api/event";
import type { Bridge, Snapshot, VmStatus } from "./types";

export const bridge: Bridge = {
  getSnapshot: () => invoke<Snapshot>("get_snapshot"),
  setResources: (resources) => invoke("set_resources", { resources }),
  onStatus: (listener) => {
    void listen<VmStatus>("vm-status", (event) => listener(event.payload));
  },
};
