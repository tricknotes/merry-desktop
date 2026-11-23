import { Call, Events } from "@wailsio/runtime";
import "./style.css";

type SetupState = "running" | "done" | "skipped" | "failed";

interface SetupEvent {
  step: string;
  state: SetupState;
  message: string;
}

const MARKS: Record<SetupState, string> = {
  running: "…",
  done: "✓",
  skipped: "–",
  failed: "✗",
};

const runButton = document.querySelector<HTMLButtonElement>("#run")!;
const resultEl = document.querySelector<HTMLParagraphElement>("#result")!;

function show(event: SetupEvent): void {
  const item = document.querySelector<HTMLLIElement>(`li[data-step="${event.step}"]`);
  if (!item) return;
  item.dataset.state = event.state;
  item.querySelector(".mark")!.textContent = MARKS[event.state];
  item.querySelector(".detail")!.textContent = event.message;
}

function reset(): void {
  document.querySelectorAll<HTMLLIElement>("li[data-step]").forEach((item) => {
    delete item.dataset.state;
    item.querySelector(".mark")!.textContent = "";
    item.querySelector(".detail")!.textContent = "";
  });
}

Events.On("setup:event", (event) => show(event.data as SetupEvent));

runButton.addEventListener("click", async () => {
  runButton.disabled = true;
  resultEl.textContent = "";
  reset();
  try {
    await Call.ByName("main.VMService.RunSetup");
    resultEl.textContent = "セットアップが完了しました。ターミナルから docker を使えます。";
    runButton.textContent = "もう一度実行";
  } catch (error) {
    resultEl.textContent = "セットアップが途中で止まりました。表示された内容を確認して、もう一度実行してください。";
    runButton.textContent = "もう一度実行";
    console.error(error);
  } finally {
    runButton.disabled = false;
  }
});
