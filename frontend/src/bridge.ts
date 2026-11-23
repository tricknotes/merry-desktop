import { Call, Events } from "@wailsio/runtime";
import type { Bridge, Snapshot } from "./types";

// Bound methods are addressed by "<package path>.<type>.<method>".
const SERVICE = "main.VMService";

export const bridge: Bridge = {
  getSnapshot: () => Call.ByName(`${SERVICE}.GetSnapshot`) as Promise<Snapshot>,
  setResources: async (resources) => {
    await Call.ByName(`${SERVICE}.SetResources`, resources);
  },
  onChange: (listener) => {
    Events.On("vm:changed", (event) => listener(event.data as Snapshot));
  },
};
