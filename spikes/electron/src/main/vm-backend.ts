/** VM state as reported by a backend. */
export type VmStatus = "stopped" | "starting" | "running" | "error";

export interface Resources {
  cpus: number;
  memoryMiB: number;
}

export interface ExecResult {
  exitCode: number;
  stdout: string;
  stderr: string;
}

/**
 * OS-specific VM operations. Implementations must not leak backend-specific
 * concepts (WSL distro names, VHDX paths, Lima instance names) through this API.
 */
export interface VmBackend {
  /** Boots the VM and resolves once the container engine responds. */
  start(): Promise<void>;
  stop(): Promise<void>;
  exec(args: string[]): Promise<ExecResult>;
  status(): Promise<VmStatus>;
  setResources(resources: Resources): Promise<void>;
}
