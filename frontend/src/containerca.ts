import { call } from "./native";

const box = document.querySelector<HTMLInputElement>("#container-ca")!;
const messageEl = document.querySelector<HTMLParagraphElement>("#container-ca-message")!;

box.addEventListener("change", async () => {
  const enable = box.checked;
  box.disabled = true;
  messageEl.textContent = "";
  try {
    await call("SetContainerCA", enable);
    messageEl.textContent = enable
      ? "オンにしました。これから作るコンテナに反映されます。"
      : "オフにしました。これから作るコンテナには渡しません。";
  } catch (error) {
    box.checked = !enable;
    messageEl.textContent = String(error);
  } finally {
    box.disabled = false;
  }
});

export async function initContainerCA(): Promise<void> {
  box.checked = await call<boolean>("ContainerCAEnabled");
}
