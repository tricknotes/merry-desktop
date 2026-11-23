import { call } from "./native";

interface ProjectUsage {
  project: string;
  containers: number;
  cpuPercent: number;
  memoryBytes: number;
}

interface Usage {
  projects: ProjectUsage[] | null;
  memoryBytes: number;
  memTotalBytes: number;
}

const table = document.querySelector<HTMLTableElement>("#usage")!;
const body = table.querySelector("tbody")!;
const messageEl = document.querySelector<HTMLParagraphElement>("#usage-message")!;
const refreshEl = document.querySelector<HTMLButtonElement>("#usage-refresh")!;

/** How often the table refreshes while the window is visible. */
const INTERVAL_MS = 10_000;

function formatBytes(n: number): string {
  if (n >= 2 ** 30) return `${(n / 2 ** 30).toFixed(1)} GiB`;
  if (n >= 2 ** 20) return `${Math.round(n / 2 ** 20)} MiB`;
  return `${Math.round(n / 2 ** 10)} KiB`;
}

function row(cells: string[]): HTMLTableRowElement {
  const tr = document.createElement("tr");
  for (const text of cells) {
    const td = document.createElement("td");
    td.textContent = text;
    tr.append(td);
  }
  return tr;
}

function render(u: Usage): void {
  const projects = u.projects ?? [];
  table.hidden = projects.length === 0;
  body.replaceChildren(
    ...projects.map((p) =>
      row([
        p.project === "" ? "(Compose 以外)" : p.project,
        String(p.containers),
        `${p.cpuPercent.toFixed(1)}%`,
        formatBytes(p.memoryBytes),
      ]),
    ),
  );
  if (projects.length === 0) {
    messageEl.textContent = "動いているコンテナはありません。";
  } else if (u.memTotalBytes > 0) {
    const share = Math.round((u.memoryBytes / u.memTotalBytes) * 100);
    messageEl.textContent = `合計 ${formatBytes(u.memoryBytes)}(VM のメモリ ${formatBytes(u.memTotalBytes)} の ${share}%)。CPU は 100% が 1 コア分です。`;
  } else {
    messageEl.textContent = "";
  }
}

async function refresh(): Promise<void> {
  refreshEl.disabled = true;
  try {
    render(await call<Usage>("ProjectUsage"));
  } catch (error) {
    table.hidden = true;
    messageEl.textContent = `取得できませんでした: ${String(error)}`;
  } finally {
    refreshEl.disabled = false;
  }
}

export function initUsage(): void {
  refreshEl.addEventListener("click", () => void refresh());
  void refresh();
  setInterval(() => {
    if (document.visibilityState === "visible") void refresh();
  }, INTERVAL_MS);
}
