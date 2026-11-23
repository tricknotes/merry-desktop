import { call } from "./native";

interface Vulnerability {
  id: string;
  package: string;
  installed: string;
  fixed: string;
  severity: string;
  title: string;
  target: string;
}

interface ScanResult {
  image: string;
  counts: Record<string, number>;
  vulnerabilities: Vulnerability[];
  truncated: boolean;
}

const SEVERITIES = ["CRITICAL", "HIGH", "MEDIUM", "LOW", "UNKNOWN"];

const form = document.querySelector<HTMLFormElement>("#scan")!;
const input = document.querySelector<HTMLInputElement>("#scan-image")!;
const images = document.querySelector<HTMLDataListElement>("#scan-images")!;
const messageEl = document.querySelector<HTMLParagraphElement>("#scan-message")!;
const table = document.querySelector<HTMLTableElement>("#scan-result")!;
const body = table.querySelector("tbody")!;
const button = form.querySelector<HTMLButtonElement>("button")!;

function cell(text: string, title?: string): HTMLTableCellElement {
  const td = document.createElement("td");
  td.textContent = text;
  if (title) td.title = title;
  return td;
}

function render(r: ScanResult): void {
  const total = SEVERITIES.reduce((n, s) => n + (r.counts[s] ?? 0), 0);
  if (total === 0) {
    messageEl.textContent = `${r.image}: 既知の脆弱性は見つかりませんでした。`;
    table.hidden = true;
    return;
  }
  const summary = SEVERITIES.filter((s) => r.counts[s]).map((s) => `${s} ${r.counts[s]}`);
  messageEl.textContent =
    `${r.image}: ${summary.join("、")}` + (r.truncated ? `(深刻なものから ${r.vulnerabilities.length} 件を表示)` : "");
  body.replaceChildren(
    ...r.vulnerabilities.map((v) => {
      const tr = document.createElement("tr");
      tr.dataset.severity = v.severity;
      tr.append(
        cell(v.severity),
        cell(v.id, v.title),
        cell(v.package, v.target),
        cell(v.installed),
        cell(v.fixed || "なし"),
      );
      return tr;
    }),
  );
  table.hidden = false;
}

async function loadImages(): Promise<void> {
  try {
    const list = (await call<string[] | null>("LocalImages")) ?? [];
    images.replaceChildren(
      ...list.map((name) => {
        const option = document.createElement("option");
        option.value = name;
        return option;
      }),
    );
  } catch {
    // The VM may be stopped; the field still accepts a name.
  }
}

form.addEventListener("submit", async (event) => {
  event.preventDefault();
  button.disabled = true;
  table.hidden = true;
  messageEl.textContent = "スキャンしています…";
  try {
    render(await call<ScanResult>("ScanImage", input.value));
  } catch (error) {
    messageEl.textContent = String(error);
  } finally {
    button.disabled = false;
  }
});

export function initScan(): void {
  input.addEventListener("focus", () => void loadImages());
  void loadImages();
}
