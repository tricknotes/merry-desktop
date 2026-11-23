import { call } from "./native";

interface Integration {
  name: string;
  default: boolean;
  enabled: boolean;
}

const listEl = document.querySelector<HTMLUListElement>("#integrations")!;
const saveEl = document.querySelector<HTMLButtonElement>("#save-integrations")!;
const resetEl = document.querySelector<HTMLButtonElement>("#reset-integrations")!;
const messageEl = document.querySelector<HTMLParagraphElement>("#integrations-message")!;

function render(items: Integration[]): void {
  if (items.length === 0) {
    listEl.innerHTML = "<li>連携できる WSL2 のディストリがありません</li>";
    saveEl.disabled = resetEl.disabled = true;
    return;
  }
  saveEl.disabled = resetEl.disabled = false;
  listEl.replaceChildren(
    ...items.map((item) => {
      const li = document.createElement("li");
      const label = document.createElement("label");
      const box = document.createElement("input");
      box.type = "checkbox";
      box.value = item.name;
      box.checked = item.enabled;
      label.append(box, ` ${item.name}${item.default ? "(既定のディストリ)" : ""}`);
      li.append(label);
      return li;
    }),
  );
}

async function load(): Promise<void> {
  render((await call<Integration[] | null>("Integrations")) ?? []);
}

async function apply(names: string[] | null, done: string): Promise<void> {
  messageEl.textContent = "反映しています…";
  saveEl.disabled = resetEl.disabled = true;
  try {
    await call("SetIntegrations", names);
    messageEl.textContent = done;
    await load();
  } catch (error) {
    messageEl.textContent = `反映できませんでした: ${String(error)}`;
    saveEl.disabled = resetEl.disabled = false;
  }
}

saveEl.addEventListener("click", () => {
  const names = [...listEl.querySelectorAll<HTMLInputElement>("input:checked")].map((b) => b.value);
  void apply(names, "保存しました。動いているコンテナはそのままです");
});

resetEl.addEventListener("click", () => {
  void apply(null, "既定(既定のディストリだけ)に戻しました");
});

export async function initIntegrations(): Promise<void> {
  try {
    await load();
  } catch (error) {
    messageEl.textContent = String(error);
  }
}
