import { bridge } from "./bridge";
import type { VmStatus } from "./types";
import "./style.css";

// Changed by the benchmark script to measure incremental frontend builds.
const BENCH_REVISION = 0;

const STATUS_LABELS: Record<VmStatus, string> = {
  stopped: "停止中",
  starting: "起動中",
  running: "稼働中",
  error: "異常",
};

const statusEl = document.querySelector<HTMLSpanElement>("#status")!;
const form = document.querySelector<HTMLFormElement>("#settings")!;
const cpusEl = document.querySelector<HTMLInputElement>("#cpus")!;
const memoryEl = document.querySelector<HTMLInputElement>("#memory")!;
const messageEl = document.querySelector<HTMLParagraphElement>("#message")!;

function showStatus(status: VmStatus): void {
  statusEl.dataset.status = status;
  statusEl.textContent = STATUS_LABELS[status];
}

form.addEventListener("submit", async (event) => {
  event.preventDefault();
  messageEl.textContent = "";
  try {
    await bridge.setResources({
      cpus: Number(cpusEl.value),
      memoryMiB: Number(memoryEl.value),
    });
    messageEl.textContent = "保存しました";
  } catch (error) {
    messageEl.textContent = `保存できませんでした: ${String(error)}`;
  }
});

async function init(): Promise<void> {
  console.debug(`bench revision ${BENCH_REVISION}`);
  bridge.onStatus(showStatus);
  const snapshot = await bridge.getSnapshot();
  showStatus(snapshot.status);
  cpusEl.value = String(snapshot.resources.cpus);
  memoryEl.value = String(snapshot.resources.memoryMiB);
}

void init();
