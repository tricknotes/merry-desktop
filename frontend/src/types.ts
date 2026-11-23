export type VmStatus = "stopped" | "starting" | "running" | "error";

export interface Resources {
  cpus: number;
  memoryMiB: number;
}

export interface Snapshot {
  status: VmStatus;
  resources: Resources;
  /** Explains an error status. Empty otherwise. */
  message: string;
}

/** Native side of the app. */
export interface Bridge {
  getSnapshot(): Promise<Snapshot>;
  setResources(resources: Resources): Promise<void>;
  onChange(listener: (snapshot: Snapshot) => void): void;
}
