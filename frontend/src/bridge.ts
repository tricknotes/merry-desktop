import { call, on } from "./native";
import type { Bridge, ResourcePlan, Snapshot } from "./types";

export const bridge: Bridge = {
  getSnapshot: () => call<Snapshot>("GetSnapshot"),
  planResources: (resources) => call<ResourcePlan>("PlanResources", resources),
  applyResources: async (resources, restartNow) => {
    await call("ApplyResources", resources, restartNow);
  },
  onChange: (listener) => on("vm:changed", (data) => listener(data as Snapshot)),
  start: async () => {
    await call("Start");
  },
  stop: async () => {
    await call("Stop");
  },
  restart: async () => {
    await call("Restart");
  },
};
