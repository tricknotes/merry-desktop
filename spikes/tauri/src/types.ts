export type VmStatus = "stopped" | "starting" | "running" | "error";

export interface Resources {
  cpus: number;
  memoryMiB: number;
}

export interface Snapshot {
  status: VmStatus;
  resources: Resources;
}

/** Native side of the app, implemented per framework in bridge.ts. */
export interface Bridge {
  getSnapshot(): Promise<Snapshot>;
  setResources(resources: Resources): Promise<void>;
  onStatus(listener: (status: VmStatus) => void): void;
}
