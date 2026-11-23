import { bridge } from "./bridge";
import type { Snapshot, VmStatus } from "./types";
import "./style.css";

const STATUS_LABELS: Record<VmStatus, string> = {
  stopped: "停止中",
  starting: "起動中",
  running: "稼働中",
  error: "異常",
};

const statusEl = document.querySelector<HTMLSpanElement>("#status")!;
const detailEl = document.querySelector<HTMLParagraphElement>("#status-detail")!;
const form = document.querySelector<HTMLFormElement>("#settings")!;
const cpusEl = document.querySelector<HTMLInputElement>("#cpus")!;
const memoryEl = document.querySelector<HTMLInputElement>("#memory")!;
const messageEl = document.querySelector<HTMLParagraphElement>("#message")!;

function show(snapshot: Snapshot): void {
  statusEl.dataset.status = snapshot.status;
  statusEl.textContent = STATUS_LABELS[snapshot.status];
  detailEl.textContent = snapshot.message;
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
  bridge.onChange(show);
  const snapshot = await bridge.getSnapshot();
  show(snapshot);
  cpusEl.value = String(snapshot.resources.cpus);
  memoryEl.value = String(snapshot.resources.memoryMiB);
}

void init();
