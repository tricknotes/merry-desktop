import { EventEmitter } from "node:events";
import type { Resources, VmBackend, VmStatus } from "./vm-backend";

// Changed by the benchmark script to measure incremental backend builds.
export const BENCH_REVISION = 0;

export interface Snapshot {
  status: VmStatus;
  resources: Resources;
}

/** OS-independent core: owns the UI-facing state and drives the backend. */
export class VmController extends EventEmitter<{ status: [VmStatus] }> {
  private current: VmStatus = "stopped";
  private resources: Resources = { cpus: 2, memoryMiB: 4096 };

  constructor(private readonly backend: VmBackend) {
    super();
  }

  status(): VmStatus {
    return this.current;
  }

  snapshot(): Snapshot {
    return { status: this.current, resources: { ...this.resources } };
  }

  async start(): Promise<void> {
    if (this.current === "starting" || this.current === "running") return;
    this.setStatus("starting");
    try {
      await this.backend.start();
      this.setStatus("running");
    } catch (error) {
      console.error(error);
      this.setStatus("error");
    }
  }

  async stop(): Promise<void> {
    try {
      await this.backend.stop();
      this.setStatus("stopped");
    } catch (error) {
      console.error(error);
      this.setStatus("error");
    }
  }

  async setResources(resources: Resources): Promise<void> {
    await this.backend.setResources(resources);
    this.resources = { ...resources };
  }

  private setStatus(status: VmStatus): void {
    this.current = status;
    this.emit("status", status);
  }
}
