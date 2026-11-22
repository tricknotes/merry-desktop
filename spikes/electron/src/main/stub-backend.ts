import type { ExecResult, Resources, VmBackend, VmStatus } from "./vm-backend";

const START_DELAY_MS = 1500;

/** In-memory backend used by the spike. No VM is touched. */
export class StubBackend implements VmBackend {
  private state: VmStatus = "stopped";
  private resources: Resources = { cpus: 2, memoryMiB: 4096 };

  async start(): Promise<void> {
    this.state = "starting";
    await new Promise((resolve) => setTimeout(resolve, START_DELAY_MS));
    if (process.env.SPIKE_STUB_FAIL_START === "1") {
      this.state = "error";
      throw new Error("stub: start failed (SPIKE_STUB_FAIL_START=1)");
    }
    this.state = "running";
  }

  async stop(): Promise<void> {
    this.state = "stopped";
  }

  async exec(args: string[]): Promise<ExecResult> {
    if (this.state !== "running") {
      throw new Error("stub: VM is not running");
    }
    return { exitCode: 0, stdout: `stub: ${args.join(" ")}`, stderr: "" };
  }

  async status(): Promise<VmStatus> {
    return this.state;
  }

  async setResources(resources: Resources): Promise<void> {
    if (!Number.isInteger(resources.cpus) || resources.cpus < 1) {
      throw new Error("cpus must be an integer >= 1");
    }
    if (!Number.isInteger(resources.memoryMiB) || resources.memoryMiB < 1024) {
      throw new Error("memoryMiB must be an integer >= 1024");
    }
    this.resources = { ...resources };
  }

  /** Stub-only helper so the core can show the stored values. */
  currentResources(): Resources {
    return { ...this.resources };
  }
}
