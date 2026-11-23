import { call } from "./native";

interface CleanupItem {
  id: string;
  name: string;
  project: string;
  sizeBytes: number;
  detail: string;
  inUse: boolean;
}

interface CleanupReport {
  containers: CleanupItem[];
  images: CleanupItem[];
  volumes: CleanupItem[];
  buildCacheBytes: number;
}

const autoEl = document.querySelector<HTMLInputElement>("#auto-cleanup")!;
const loadEl = document.querySelector<HTMLButtonElement>("#cleanup-load")!;
const box = document.querySelector<HTMLDivElement>("#cleanup")!;
const containersEl = document.querySelector<HTMLUListElement>("#cleanup-containers")!;
const imagesEl = document.querySelector<HTMLUListElement>("#cleanup-images")!;
const volumesEl = document.querySelector<HTMLUListElement>("#cleanup-volumes")!;
const removeEl = document.querySelector<HTMLButtonElement>("#cleanup-remove")!;
const messageEl = document.querySelector<HTMLParagraphElement>("#cleanup-message")!;

function formatBytes(n: number): string {
  if (n >= 2 ** 30) return `${(n / 2 ** 30).toFixed(1)} GiB`;
  if (n >= 2 ** 20) return `${Math.round(n / 2 ** 20)} MiB`;
  return `${Math.round(n / 2 ** 10)} KiB`;
}

function describe(item: CleanupItem): string {
  const parts = [item.name, formatBytes(item.sizeBytes)];
  if (item.project) parts.push(`プロジェクト ${item.project}`);
  if (item.detail) parts.push(item.detail);
  return parts.join(" ・ ");
}

/** Fills a list; with checkboxes when the items can be removed here. */
function fill(list: HTMLUListElement, items: CleanupItem[], selectable: boolean): void {
  if (items.length === 0) {
    const li = document.createElement("li");
    li.textContent = "ありません";
    list.replaceChildren(li);
    return;
  }
  list.replaceChildren(
    ...items.map((item) => {
      const li = document.createElement("li");
      const text = document.createElement("span");
      text.textContent = describe(item);
      if (selectable) {
        const label = document.createElement("label");
        label.className = "check";
        const box = document.createElement("input");
        box.type = "checkbox";
        box.value = item.id;
        label.append(box, text);
        li.append(label);
      } else {
        li.append(text);
      }
      return li;
    }),
  );
}

const checked = (list: HTMLUListElement): string[] =>
  [...list.querySelectorAll<HTMLInputElement>("input:checked")].map((b) => b.value);

async function load(): Promise<void> {
  loadEl.disabled = true;
  messageEl.textContent = "調べています…";
  try {
    const r = await call<CleanupReport>("CleanupCandidates");
    fill(containersEl, r.containers, true);
    fill(imagesEl, r.images, true);
    fill(volumesEl, r.volumes, false);
    removeEl.disabled = r.containers.length + r.images.length === 0;
    box.hidden = false;
    messageEl.textContent = `ビルドキャッシュは ${formatBytes(r.buildCacheBytes)}(自動の整理の対象)。`;
  } catch (error) {
    box.hidden = true;
    messageEl.textContent = String(error);
  } finally {
    loadEl.disabled = false;
  }
}

removeEl.addEventListener("click", async () => {
  const containers = checked(containersEl);
  const images = checked(imagesEl);
  if (containers.length + images.length === 0) {
    messageEl.textContent = "削除するものを選んでください。";
    return;
  }
  const ok = window.confirm(
    `コンテナ ${containers.length} 個とイメージ ${images.length} 個を削除します。元には戻せません。よろしいですか?`,
  );
  if (!ok) return;
  removeEl.disabled = true;
  try {
    await call("RemoveUnused", containers, images);
    await load();
    messageEl.textContent = "削除しました。" + messageEl.textContent;
  } catch (error) {
    messageEl.textContent = String(error);
    removeEl.disabled = false;
  }
});

autoEl.addEventListener("change", async () => {
  try {
    await call("SetAutoCleanup", autoEl.checked);
  } catch (error) {
    autoEl.checked = !autoEl.checked;
    messageEl.textContent = String(error);
  }
});

export async function initCleanup(): Promise<void> {
  loadEl.addEventListener("click", () => void load());
  autoEl.checked = await call<boolean>("AutoCleanupEnabled");
}
