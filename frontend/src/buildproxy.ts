import { call } from "./native";

const box = document.querySelector<HTMLInputElement>("#build-proxy")!;
const resultEl = document.querySelector<HTMLUListElement>("#build-proxy-result")!;

function show(lines: string[]): void {
  resultEl.replaceChildren(
    ...lines.map((line) => {
      const li = document.createElement("li");
      li.textContent = line;
      return li;
    }),
  );
}

box.addEventListener("change", async () => {
  const enable = box.checked;
  box.disabled = true;
  show(["反映しています…"]);
  try {
    show((await call<string[] | null>("SetBuildProxy", enable)) ?? []);
  } catch (error) {
    box.checked = !enable;
    show([String(error)]);
  } finally {
    box.disabled = false;
  }
});

export async function initBuildProxy(): Promise<void> {
  box.checked = await call<boolean>("BuildProxyEnabled");
}
