import { bridge } from "./bridge";
import { initRegistries } from "./registry";
import type { Resources, Snapshot, VmStatus } from "./types";
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
const diskEl = document.querySelector<HTMLInputElement>("#disk")!;
const messageEl = document.querySelector<HTMLParagraphElement>("#message")!;

function show(snapshot: Snapshot): void {
  statusEl.dataset.status = snapshot.status;
  statusEl.textContent = STATUS_LABELS[snapshot.status];
  detailEl.textContent = snapshot.message;
}

/** An empty field means "default", sent as 0. */
const valueOf = (el: HTMLInputElement): number => (el.value.trim() === "" ? 0 : Number(el.value));
const fieldOf = (n: number): string => (n > 0 ? String(n) : "");

function fill(r: Resources): void {
  cpusEl.value = fieldOf(r.cpus);
  memoryEl.value = fieldOf(r.memoryMiB);
  diskEl.value = fieldOf(r.diskGiB);
}

form.addEventListener("submit", async (event) => {
  event.preventDefault();
  messageEl.textContent = "";
  const resources: Resources = {
    cpus: valueOf(cpusEl),
    memoryMiB: valueOf(memoryEl),
    diskGiB: valueOf(diskEl),
  };
  try {
    const plan = await bridge.planResources(resources);
    let restartNow = false;
    if (plan.restartHost) {
      const ok = window.confirm(
        "CPU数とメモリは WSL 全体の設定(.wslconfig)です。ほかの WSL ディストリにも反映されます。\n" +
          "元の .wslconfig はバックアップしてから書き換えます。保存しますか?",
      );
      if (!ok) return;
      restartNow = window.confirm(
        "反映するには WSL 全体を再起動します。ほかの WSL ディストリも停止します。\n" +
          "今すぐ再起動しますか?(キャンセルすると、次に WSL が起動したときに反映されます)",
      );
    } else if (plan.restartVM) {
      restartNow = window.confirm(
        "ディスク上限は VM の再起動で反映されます。今すぐ再起動しますか?\n(キャンセルすると、次の起動時に反映されます)",
      );
    }
    await bridge.applyResources(resources, restartNow);
    messageEl.textContent = restartNow ? "保存して再起動しました" : "保存しました";
  } catch (error) {
    messageEl.textContent = `保存できませんでした: ${String(error)}`;
  }
});

async function init(): Promise<void> {
  bridge.onChange(show);
  const snapshot = await bridge.getSnapshot();
  show(snapshot);
  fill(snapshot.resources);
  await initRegistries();
}

void init();
