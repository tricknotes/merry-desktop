import { Call, Events } from "@wailsio/runtime";
import type { Bridge, ResourcePlan, Snapshot } from "./types";

// Bound methods are addressed by "<package path>.<type>.<method>".
const SERVICE = "main.VMService";

export const bridge: Bridge = {
  getSnapshot: () => Call.ByName(`${SERVICE}.GetSnapshot`) as Promise<Snapshot>,
  planResources: (resources) =>
    Call.ByName(`${SERVICE}.PlanResources`, resources) as Promise<ResourcePlan>,
  applyResources: async (resources, restartNow) => {
    await Call.ByName(`${SERVICE}.ApplyResources`, resources, restartNow);
  },
  onChange: (listener) => {
    Events.On("vm:changed", (event) => listener(event.data as Snapshot));
  },
};
