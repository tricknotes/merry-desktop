// Summarizes phase 0 benchmark results (bench-*.json, memory-*.json) as a Markdown table.
// Usage: node scripts/phase0/summarize.mjs <results-dir>
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";

const dir = process.argv[2];
if (!dir) {
  console.error("usage: node summarize.mjs <results-dir>");
  process.exit(2);
}

function walk(path) {
  return readdirSync(path).flatMap((name) => {
    const full = join(path, name);
    return statSync(full).isDirectory() ? walk(full) : [full];
  });
}

// PowerShell writes UTF-8 with BOM on Windows PowerShell; strip it.
const readJson = (file) => JSON.parse(readFileSync(file, "utf8").replace(/^﻿/, ""));

const files = walk(dir).filter((f) => f.endsWith(".json"));
const bench = files.filter((f) => /bench-[^/\\]+\.json$/.test(f)).map(readJson);
const memory = files.filter((f) => /memory-[^/\\]+\.json$/.test(f)).map(readJson);

const median = (values) => {
  const sorted = values.filter((v) => typeof v === "number").sort((a, b) => a - b);
  if (sorted.length === 0) return undefined;
  const mid = Math.floor(sorted.length / 2);
  return sorted.length % 2 ? sorted[mid] : (sorted[mid - 1] + sorted[mid]) / 2;
};
const sec = (v) => (v === undefined ? "—" : `${v.toFixed(1)} 秒`);
const mb = (v) => (v === undefined ? "—" : `${(v / 1024 / 1024).toFixed(1)} MB`);
const mbRaw = (v) => (v === undefined ? "—" : `${v.toFixed(1)} MB`);

const candidates = ["tauri", "electron", "wails"];
const by = (rows, key) => (c) => rows.filter((r) => (r[key] ?? "").startsWith(c));

const lines = [];
lines.push("| 項目 | " + candidates.join(" | ") + " |");
lines.push("| --- |" + candidates.map(() => " --- |").join(""));
const row = (label, fn) => lines.push(`| ${label} | ${candidates.map(fn).join(" | ")} |`);
const benchOf = by(bench, "candidate");
row("計測回数", (c) => String(benchOf(c).length));
row("依存の取得", (c) => sec(median(benchOf(c).map((r) => r.depsSec))));
row("初回ビルド", (c) => sec(median(benchOf(c).map((r) => r.cleanBuildSec))));
row("初回合計(依存+ビルド)", (c) => sec(median(benchOf(c).map((r) => r.depsSec + r.cleanBuildSec))));
row("差分ビルド(バックエンド)", (c) => sec(median(benchOf(c).map((r) => r.backendIncrSec))));
row("差分ビルド(フロント)", (c) => sec(median(benchOf(c).map((r) => r.frontendIncrSec))));
row("実行ファイル一式", (c) => mb(median(benchOf(c).map((r) => r.sizeBytes))));
row("同(zip圧縮後)", (c) => mb(median(benchOf(c).map((r) => r.zipBytes))));
const memOf = by(memory, "label");
row("常駐メモリ Working Set", (c) => mbRaw(median(memOf(c).map((r) => r.workingSetMB))));
row("常駐メモリ Private Bytes", (c) => mbRaw(median(memOf(c).map((r) => r.privateMB))));

lines.push("");
lines.push("各値は中央値。差分ビルド(フロント)はリリースビルドのやり直しで、開発サーバーのホットリロードではない。");
const runners = [...new Set(bench.map((r) => `${r.runner} / ${r.cpu} / ${r.logicalCpus} 論理CPU`))];
if (runners.length) lines.push("", "計測環境: " + runners.join("; "));

console.log(lines.join("\n"));
