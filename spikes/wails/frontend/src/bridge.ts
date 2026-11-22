import { Call, Events } from "@wailsio/runtime";
import type { Bridge, Snapshot, VmStatus } from "./types";

// Bound methods are addressed by "<package>.<type>.<method>".
const SERVICE = "main.VmService";

export const bridge: Bridge = {
  getSnapshot: () => Call.ByName(`${SERVICE}.GetSnapshot`) as Promise<Snapshot>,
  setResources: async (resources) => {
    await Call.ByName(`${SERVICE}.SetResources`, resources);
  },
  onStatus: (listener) => {
    Events.On("vm-status", (event) => listener(event.data as VmStatus));
  },
};
