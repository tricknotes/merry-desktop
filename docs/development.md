# 開発環境

ビルドに必要なツール(Go、Node.js、shellcheck)は、ホストに入れずにコンテナで使う。
`compose.yaml` の `builder` サービスが、Linux から Windows 向けの実行ファイルをクロスコンパイルする。

## 使い方

```bash
export DEV_UID=$(id -u) DEV_GID=$(id -g)   # できたファイルを自分の所有にする
docker compose build builder                # 初回だけ(Go 1.27.4 + Node.js 26.13.1)
docker compose run --rm builder make all    # lint、テスト、ビルド
docker compose run --rm builder make build  # ビルドだけ
```

| ターゲット | 内容 |
| --- | --- |
| `make lint` | シェルスクリプトの shellcheck と `gofmt` |
| `make test` | `internal/` の単体テスト(Linux、`-race` 付き)と、Windows 向けの `go vet` |
| `make build` | `bin/merry-desktop.exe` を作る |
| `make tidy` | `go mod tidy`(Windows 向けの依存で) |
| `make clean` | ビルド結果を消す |

`cmd/` のアプリ本体は Windows 向けにだけビルドする。Linux 向けの Wails は GTK が必要で、ビルド用イメージに入れていないため。

## 構成

| パス | 中身 |
| --- | --- |
| `cmd/merry-desktop/` | トレイ常駐アプリ(Wails v3)。OS ごとのバックエンドの選択だけを持つ |
| `internal/core/` | OS に依存しないコア。状態の管理と、VM 操作の直列化 |
| `internal/vm/` | `Backend` インターフェース(起動 / 停止 / コマンド実行 / 状態取得 / リソース設定) |
| `internal/vm/wsl/` | WSL2 の実装(`wsl.exe` を呼ぶ) |
| `internal/vm/stub/` | メモリ上のスタブ(開発とテスト用) |
| `internal/appinfo/` | アプリ名などの名前。アプリ名が決まったら、ここだけを変える |
| `frontend/` | 設定ウィンドウ(Vite + TypeScript) |

## 実機で試すとき

CI の成果物 `windows-binaries` の `merry-desktop.exe` を起動する。VM がまだなければ、セットアップウィンドウが開く(F-10)。
セットアップは次を順に行う。

1. WSL2 が使えるかの確認(`wsl --status`)
2. ホストのプロキシ(環境変数、なければ WinINET)と、信頼されたルート証明書の読み取り
3. 専用ディストリ `merry-desktop-engine` の作成(`%LOCALAPPDATA%\merry-desktop-engine` に置く)
4. VM の起動
5. `docker run --rm hello-world`

フェーズ0のスクリプトで作ったディストリを使うときは、環境変数 `MERRY_ENGINE_NAME` で指定する。

```powershell
$env:MERRY_ENGINE_NAME = "phase0-engine"
.\merry-desktop.exe
```

## 動作確認

作業用PCは Smart App Control で署名のない exe を起動できないため、動作確認は CI で行う(2026-11-23 決定)。
実機での確認は、別のPCで行う。
