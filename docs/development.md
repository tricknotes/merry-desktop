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
| `cmd/engine-agent/` | VM の中で動く Linux 用の中継。アプリに埋め込み、起動時に VM へ入れる |
| `internal/core/` | OS に依存しないコア。状態、操作の直列化、セットアップ、リソース、ログイン、診断 |
| `internal/vm/` | `Backend` インターフェースと、任意の追加機能(セットアップ、リソース、診断など) |
| `internal/vm/wsl/` | WSL2 の実装(`wsl.exe`、名前付きパイプ、`.wslconfig`、Windows の CLI と PATH) |
| `internal/vm/stub/` | メモリ上のスタブ(開発とテスト用) |
| `internal/mux/` | 1本の接続の上に複数の接続を流す仕組み(ホスト ⇔ engine-agent) |
| `internal/engineapi/` | Engine API の中継、バインドマウントのパス変換、コンテナへのプロキシの受け渡し |
| `internal/diag/` | 診断レポートの zip と、秘密情報の除去 |
| `internal/settings/`、`internal/applog/` | アプリの設定(JSON)とログファイル |
| `internal/appinfo/` | アプリ名などの名前。アプリ名が決まったら、ここだけを変える |
| `internal/appservice/` | 画面から呼ぶメソッド。アプリ(Wails)と開発用 UI(HTTP)で共通 |
| `internal/devui/` | 開発用 UI の HTTP サーバー(`merry-dev ui`) |
| `frontend/` | 設定ウィンドウとセットアップウィンドウ(Vite + TypeScript) |

## 機能と実装の対応

| 機能 | 主な実装 |
| --- | --- |
| F-01, F-02 | `internal/core/controller.go`、`cmd/merry-desktop/main.go`(トレイ) |
| F-03 | `internal/vm/wsl/resources.go`、`wslconfig.go` |
| F-04 | `cmd/engine-agent`、`internal/engineapi`、`internal/vm/wsl/endpoint*.go`、`distros.go`、`hostcli*.go` |
| F-05 | `cmd/merry-desktop/main.go`(Wails の Autostart) |
| F-10 | `internal/core/setup.go`、`internal/vm/wsl/provision.go` |
| F-11 | `internal/vm/wsl/hostnet*.go`、`assets/apply-network.sh`、`engineapi.InjectEnv` |
| F-12 | `internal/core/registry.go`、`internal/vm/wsl/hostcli.go`(`Login`) |
| F-14 | `internal/core/diagnostics.go`、`internal/diag`、`internal/vm/wsl/diagnostics.go` |

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

## 開発用 CLI(merry-dev)

`cmd/merry-dev` は、WSL の中(開発に使う Ubuntu など)から VM を操作する Linux 用のコマンド。
アプリと同じコアと WSL バックエンドを使い、`wsl.exe` を呼んで VM を動かす。
Windows 側には何も置かないので、署名のない exe を起動できない環境でも使える。

```bash
docker compose run --rm builder make dev   # bin/merry-dev を作る
./bin/merry-dev setup        # VM の作成から hello-world まで(初回は約20秒)
./bin/merry-dev start        # 起動して、Ctrl+C まで動かしたままにする
docker --context merry-dev-engine ps   # start の間、このディストリの docker CLI から使える
./bin/merry-dev status
./bin/merry-dev resources [-cpus N] [-memory MiB] [-disk GiB] [-restart]
./bin/merry-dev diagnostics [-o report.zip]
./bin/merry-dev stop
./bin/merry-dev ui            # 設定画面とセットアップ画面を Windows のブラウザで開く
./bin/merry-dev unregister -yes   # VM を中身ごと消す
```

- 既定の VM 名は `merry-dev-engine`。アプリの VM(`merry-desktop-engine`)とは別になる。`-name` で変えられる。
- `start` の間は、WSL の既定のディストリと連携する(ルートのバインドマウントと、docker CLI があれば context の作成)。
  `-v $PWD:/src` や Compose の `./` のバインドマウントも、このディストリのパスのまま使える。
- `.wslconfig` と VM のディスクは、Windows のユーザーフォルダ(`%USERPROFILE%`、`%LOCALAPPDATA%`)に置く。
- `resources -restart` で CPU とメモリを反映すると、WSL 全体が再起動する(ほかのディストリも止まる)。

### 画面の確認(merry-dev ui)

`merry-dev ui` は、アプリと同じ画面(`frontend/`)を `127.0.0.1:18765` で配信し、Windows のブラウザで開く
(WSL が localhost を Windows に転送する)。画面の操作は、本物のコアと VM に届く。

- 画面からの呼び出しは `frontend/src/native.ts` に集めてある。アプリでは Wails の呼び出し、
  `merry-dev ui` では HTTP(`internal/devui`)を使う。Go 側はどちらも `internal/appservice.Service`。
- 待ち受けはループバックだけ。API には URL のランダムなトークンと、ループバックの Host ヘッダーが要る
  (ほかのプロセスや、ブラウザで開いたほかのサイトからは操作できない)。
- トレイはない。代わりに設定画面の「起動」「停止」「再起動」と、上部のリンク(設定 / セットアップ)を使う。
- `-open=false` でブラウザを開かない。`-addr` で待ち受けを変えられる。

扱えないもの(Windows でしか動かないもの):

- トレイ(画面は `merry-dev ui` で確認できる)
- 名前付きパイプ(Windows の docker CLI からの接続)
- Windows の docker CLI、PATH、`docker login`(セットアップの「ターミナルの設定」は飛ばす)
- Windows の信頼されたルート証明書の取り込み(WSL の中からは読めない)

## 動作確認

作業用PCは Smart App Control で署名のない exe を起動できないため、動作確認は CI で行う(2026-11-23 決定)。
実機での確認は、別のPCで行う。
