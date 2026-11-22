import type { Bridge } from "./types";

declare global {
  interface Window {
    spike: Bridge;
  }
}

// Exposed by the preload script through contextBridge.
export const bridge: Bridge = window.spike;
