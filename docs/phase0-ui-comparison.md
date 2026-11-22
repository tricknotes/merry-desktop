# フェーズ0: UI技術の比較

- 状態: **計測前**(スパイクのコードと計測の仕組みまで。数値はまだない)
- 対象: Tauri 2 / Electron / Go + Wails v3
- 採否は人間が決める。ここでは推奨と理由を示すだけにする。

## 結果

CI の計測が終わったら、`phase0-ui-bench` ワークフローの `summary` の表をここに貼る。

| 項目 | Tauri | Electron | Wails |
| --- | --- | --- | --- |
| 初回ビルド時間 | 未計測 | 未計測 | 未計測 |
| 差分ビルド時間(バックエンド) | 未計測 | 未計測 | 未計測 |
| 差分ビルド時間(フロント) | 未計測 | 未計測 | 未計測 |
| 配布物サイズ | 未計測 | 未計測 | 未計測 |
| 常駐メモリ(実機) | 未計測 | 未計測 | 未計測 |
| 常駐メモリ(CI、参考) | 未計測 | 未計測 | 未計測 |

## 推奨

計測後に書く。

## 計測方法

### 分担

| 項目 | 場所 | 理由 |
| --- | --- | --- |
| 初回ビルド・差分ビルド・配布物サイズ | CI(GitHub Actions `windows-2025`) | 毎回キャッシュのない状態から始められる。3候補を同じ種類のランナーで測れる |
| 常駐メモリ | Windows 11 の実機 | CI のランナーは配布先のPCと違う。CI でも参考値は取る |

CLAUDE.md の「同一マシンで各3回」は、「同じ種類のランナーで各3回」と読み替えている。
共有ランナーのため回ごとにばらつきがあり、手元のPCでの待ち時間とも一致しない。比較の目安として扱う。

### 手順(CI)

`.github/workflows/phase0-ui-bench.yml` が、3候補 × 3回のジョブを並行して動かす。
各ジョブは `scripts/phase0/bench.ps1` で次を順に行う。

1. 依存のキャッシュ(npm、Cargo の registry、Go のモジュールとビルドキャッシュ、Electron のダウンロード)とビルド結果を消す。
2. 依存を取得する(時間を「依存の取得」として別に記録する)。
3. リリースビルドする(「初回ビルド」)。
4. 実行ファイル一式のサイズと、zip 圧縮後のサイズを測る。
5. バックエンドの `BENCH_REVISION` を書き換えて、もう一度リリースビルドする(「差分ビルド(バックエンド)」)。
6. フロントの `BENCH_REVISION` を書き換えて、もう一度リリースビルドする(「差分ビルド(フロント)」)。
7. ビルドしたアプリを起動し、5分待ってからメモリを測る(参考値)。

`summary` ジョブが、`scripts/phase0/summarize.mjs` で中央値の表を作る。

### 各候補のビルド内容

| 候補 | リリースビルドのコマンド | 配布物として測るもの |
| --- | --- | --- |
| Tauri | `tauri build --no-bundle`(フロントのビルドを含む) | `spike-tauri.exe` |
| Electron | `vite build` → `tsc` → `electron-builder --win --dir` | `win-unpacked/` 一式 |
| Wails | `vite build` → `go build -tags production -ldflags "-w -s -H windowsgui"` | `SpikeWails.exe` |

インストーラーは作らず、実行ファイル一式で比べる。
Tauri と Wails は Windows 標準の WebView2 Runtime を使うため、その分は含まない。

ビルド設定は各フレームワークの既定に合わせた。Tauri の Cargo の `profile.release` は既定のまま(LTO なし)。
Wails はテンプレートと同じく `-w -s` でシンボルを削っている。
Wails は Windows 用のリソース(`.syso`、アイコンとマニフェスト)を埋め込んでいない。数KB程度の差になる見込み。

### 差分ビルド(フロント)の扱い

開発サーバーで画面に反映されるまでの時間ではなく、リリースビルドのやり直しにかかる時間を測る。
3候補とも Vite のホットリロードを使えるため、開発中の反映時間には差が出にくいと見ている(未計測)。
リリースビルドでは、Tauri と Wails はフロントを実行ファイルに埋め込むため、バックエンドの再リンクを伴う。

### 手順(実機の常駐メモリ)

CI の成果物 `app-tauri`、`app-electron`、`app-wails` をダウンロードし、それぞれで次を実行する。

```powershell
pwsh scripts/phase0/measure-memory.ps1 -Exe <exeのパス> -Label <tauri|electron|wails> -OutFile memory-<label>.json
```

- 起動から5分待って、アプリと子プロセス(WebView2、Electron のヘルパーなど)の合計を測る。
- Working Set(タスクマネージャーの既定の表示)と Private Bytes の両方を記録する。
- 設定ウィンドウは開かない状態が基本。開いた状態も測るときは `-OpenWindowPrompt` を付ける。

**注意**: 作業用の Windows 11 では Smart App Control がオンになっており、署名のない exe の起動がブロックされる
(2026-11-23 に Electron 版で確認)。実機で測るには、この扱いを決める必要がある。

## スパイクの共通仕様

`spikes/README.md` を参照。

## 未検証のこと

- 3候補とも、Windows 上での動作(トレイ、メニュー、状態表示、設定ウィンドウ)は未確認。
  Electron はこの環境でビルドまで通したが、起動は Smart App Control に止められた。
  Tauri と Wails は、この環境に Rust と Go がないため、CI で初めてビルドする。
