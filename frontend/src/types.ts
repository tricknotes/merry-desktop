export type VmStatus = "stopped" | "starting" | "running" | "error";

/** Zero means "default" (WSL decides). */
export interface Resources {
  cpus: number;
  memoryMiB: number;
  diskGiB: number;
}

export interface ResourcePlan {
  /** The VM must restart for the change to take effect. */
  restartVM: boolean;
  /** The change applies to every VM of the kind (WSL: all distros). */
  restartHost: boolean;
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
  planResources(resources: Resources): Promise<ResourcePlan>;
  applyResources(resources: Resources, restartNow: boolean): Promise<void>;
  onChange(listener: (snapshot: Snapshot) => void): void;
  start(): Promise<void>;
  stop(): Promise<void>;
  restart(): Promise<void>;
}
