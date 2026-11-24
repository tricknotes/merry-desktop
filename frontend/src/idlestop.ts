import { call } from "./native";

const select = document.querySelector<HTMLSelectElement>("#idle-stop")!;
const messageEl = document.querySelector<HTMLParagraphElement>("#idle-stop-message")!;

select.addEventListener("change", async () => {
  const minutes = Number(select.value);
  select.disabled = true;
  messageEl.textContent = "";
  try {
    await call("SetIdleStopMinutes", minutes);
    messageEl.textContent = minutes > 0 ? `${minutes} 分で休止します。` : "休止しません。";
  } catch (error) {
    messageEl.textContent = String(error);
    select.value = String(await call<number>("IdleStopMinutes"));
  } finally {
    select.disabled = false;
  }
});

export async function initIdleStop(): Promise<void> {
  const minutes = await call<number>("IdleStopMinutes");
  if (![...select.options].some((o) => Number(o.value) === minutes)) {
    select.add(new Option(`${minutes} 分`, String(minutes)));
  }
  select.value = String(minutes);
}
