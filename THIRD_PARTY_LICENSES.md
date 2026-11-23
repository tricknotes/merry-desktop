# 依存ライブラリのライセンス

依存を追加したら、ここに追記する。MITと両立しないライセンス(GPL系など)は、追加前に確認を取る。

## アプリ本体

### Go(Windows 版の実行ファイルに入るもの)

`GOOS=windows go list -deps ./cmd/merry-desktop` で集計(2026-11-23)。

| モジュール | 版 | ライセンス |
| --- | --- | --- |
| github.com/wailsapp/wails/v3 | v3.1.0 | MIT |
| github.com/go-ole/go-ole | v1.3.0 | MIT |
| github.com/Microsoft/go-winio | v0.6.4 | MIT(名前付きパイプ、F-04) |
| git.sr.ht/~jackmordaunt/go-toast/v2 | v2.0.3 | MIT または Unlicense(Wails の Windows 通知が使う) |
| golang.org/x/sys | v0.49.0 | BSD-3-Clause |
| Go 標準ライブラリ | 1.27.4 | BSD-3-Clause |

`go.mod` にはほかに macOS / Linux 版でだけ使う間接の依存(coder/websocket、purego、godbus、x/term)があるが、
Windows 版には入らない。macOS 版を作るとき(フェーズ3)に確認する。

### engine-agent(VM の中で動く Linux 用のプログラム、`cmd/engine-agent`)

Go 標準ライブラリ(BSD-3-Clause)と、このリポジトリのコードだけでできている。アプリ本体に gzip で埋め込む。
接続の多重化(`internal/mux`)は自作した(既製の yamux は MPL-2.0 のため使わなかった)。

### セットアップ時にダウンロードするもの(F-04、F-10)

アプリには含めず、セットアップ時に取得して SHA-256 を検証する。

| ソフトウェア | 版 | ライセンス | 置き場所 |
| --- | --- | --- | --- |
| Alpine Linux minirootfs | 3.24.4 | 構成物ごと(下記「フェーズ0 WSL2」の表を参照) | VM |
| Docker Engine 静的バイナリ | 29.10.0 | Apache-2.0 | VM |
| Docker Compose(Linux) | v5.8.0 | Apache-2.0 | VM |
| docker CLI(Windows、`download.docker.com` の静的バイナリ) | 29.10.0 | Apache-2.0 | `%LOCALAPPDATA%\<VM名>\bin\docker.exe` |
| Docker Compose(Windows) | v5.8.0 | Apache-2.0 | `%USERPROFILE%\.docker\cli-plugins\docker-compose.exe`(なければ) |
| docker-credential-wincred(docker/docker-credential-helpers) | v0.9.13 | MIT | `%LOCALAPPDATA%\<VM名>\bin`(F-12) |

### 使うときに VM 内で取得するもの

| ソフトウェア | 版 | ライセンス | 入り方 |
| --- | --- | --- | --- |
| Trivy(`aquasec/trivy` イメージ) | 0.76.2(ダイジェスト固定) | Apache-2.0 | イメージのスキャン(I-02)を実行したときに pull する。配布物には入らない |

どれも Docker Desktop のものではない。

### フロント(`frontend/`)

`spikes/wails/frontend` と同じ構成(`@wailsio/runtime` MIT、Vite MIT、TypeScript Apache-2.0)。
配布物に入るのは `@wailsio/runtime` だけ。

## フェーズ0 UIスパイク(`spikes/wails`)

比較のための試作で、配布はしない。置いているのは採用した Wails のスパイクだけ。
比較に使った Tauri と Electron のスパイクは、コミット `0e51eb8` にある。

### 直接の依存

| 依存 | ライセンス | 配布物に入るか |
| --- | --- | --- |
| Wails v3.1.0 | MIT | 入る |
| `@wailsio/runtime` 3.1.0 | MIT | 入る(フロントにバンドル) |
| Vite 8.3.8 | MIT | 入らない(ビルド用) |
| TypeScript 7.0.2 | Apache-2.0 | 入らない(ビルド用) |

### 間接の依存

- npm(`spikes/wails/frontend/package-lock.json`、2026-11-23 集計): MIT 32、Apache-2.0 22、MPL-2.0 12、ISC・BSD-3-Clause・0BSD 各1。GPL系はない。
  MPL-2.0 は lightningcss(Vite が使うCSS処理)で、ビルド時だけ使う。配布物には入らない。
- Go(`spikes/wails/go.sum`): 未確認。フェーズ1でアプリ本体を作るときに `go-licenses` などで確認する。
- WebView2 Runtime は Windows の標準コンポーネントで、配布物には含めない。

## フェーズ0 WSL2 バックエンドの検証(`scripts/phase0/wsl2/`)

スクリプトが構築時にダウンロードして、Engine 用のディストリに入れるもの。リポジトリには含めていない。

| ソフトウェア | 版 | ライセンス | 入り方 |
| --- | --- | --- | --- |
| Alpine Linux minirootfs | 3.24.4 | 構成物ごとに異なる。musl は MIT、BusyBox は GPL-2.0 など | ディストリのもと |
| Docker Engine 静的バイナリ(dockerd、containerd、runc、docker CLI ほか) | 29.10.0 | Apache-2.0(docker-init の tini は MIT) | `/usr/local/bin` に配置 |
| Docker Compose | v5.8.0 | Apache-2.0 | CLI プラグインとして配置 |
| ca-certificates、curl | Alpine 3.24 のパッケージ | MPL-2.0 / curl ライセンス(MIT 系) | apk で導入 |
| iptables | Alpine 3.24 のパッケージ | GPL-2.0 | apk で導入(dockerd が使う) |
| socat | Alpine 3.24 のパッケージ | GPL-2.0 | apk で導入(名前付きパイプの中継が使う) |

### 要確認: VM の中の GPL のソフトウェア

BusyBox、iptables、socat は GPL-2.0。本アプリのコードとはリンクせず、VM の中で別のプログラムとして動く。
WSL のディストリ(Linux)を使う以上、GPL のソフトウェアを含まない構成は現実的でない。
Rancher Desktop も同じく Alpine をもとにしたディストリを配布している。

製品で rootfs を同梱して配布する場合は、GPL のソースコードの提供方法(Alpine のソースへの案内など)を決める必要がある。
採否の判断はフェーズ1の配布設計で行う。socat は、フェーズ1で中継を Go で作り直せば不要にできる見込み。
