import { call, on, showDevNav } from "./native";
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

showDevNav();
on("setup:event", (data) => show(data as SetupEvent));

const recreateButton = document.querySelector<HTMLButtonElement>("#recreate")!;

recreateButton.addEventListener("click", async () => {
  const first = window.confirm(
    "VM を削除して、最初から作り直します。\n" +
      "VM の中のイメージ、コンテナ、ボリュームはすべて消え、元に戻せません。続けますか?",
  );
  if (!first) return;
  const second = window.confirm("本当に作り直しますか? 必要なデータは先に退避してください。");
  if (!second) return;
  runButton.disabled = recreateButton.disabled = true;
  resultEl.textContent = "";
  reset();
  try {
    await call("RecreateVM");
    resultEl.textContent = "VM を作り直しました。";
  } catch (error) {
    resultEl.textContent = "作り直しが途中で止まりました。表示された内容を確認して、「開始」でセットアップをやり直してください。";
    console.error(error);
  } finally {
    runButton.disabled = recreateButton.disabled = false;
  }
});

runButton.addEventListener("click", async () => {
  runButton.disabled = true;
  resultEl.textContent = "";
  reset();
  try {
    await call("RunSetup");
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
