# 依存ライブラリのライセンス

依存を追加したら、ここに追記する。MITと両立しないライセンス(GPL系など)は、追加前に確認を取る。

## フェーズ0 UIスパイク(`spikes/`)

比較のための試作で、配布はしない。採用した技術以外は、フェーズ1に入る前に削除する。

### 直接の依存

| 依存 | 使う場所 | ライセンス | 配布物に入るか |
| --- | --- | --- | --- |
| Electron 45.4.1 | `spikes/electron` | MIT。同梱の Chromium などは個別のライセンス(後述) | 入る |
| electron-builder 26.18.0 | `spikes/electron` | MIT | 入らない(ビルド用) |
| Tauri 2.12 (`tauri`, `tauri-build`) | `spikes/tauri` | MIT OR Apache-2.0 | 入る |
| `@tauri-apps/api` 2.12.5 | `spikes/tauri` | Apache-2.0 OR MIT | 入る(フロントにバンドル) |
| `@tauri-apps/cli` 2.12.6 | `spikes/tauri` | Apache-2.0 OR MIT | 入らない(ビルド用) |
| serde 1 | `spikes/tauri` | MIT OR Apache-2.0 | 入る |
| Wails v3.1.0 | `spikes/wails` | MIT | 入る |
| `@wailsio/runtime` 3.1.0 | `spikes/wails` | MIT | 入る(フロントにバンドル) |
| Vite 8.3.8 | 3つとも | MIT | 入らない(ビルド用) |
| TypeScript 7.0.2 | 3つとも | Apache-2.0 | 入らない(ビルド用) |
| `@types/node` 24 | `spikes/electron` | MIT | 入らない(ビルド用) |

### 間接の依存(npm)

`package-lock.json` から集計した結果(2026-11-23)。GPL系はなかった。

| スパイク | 内訳 |
| --- | --- |
| electron | MIT 241、ISC 35、Apache-2.0 28、MPL-2.0 12、BSD-3-Clause 10、BlueOak-1.0.0 9、BSD-2-Clause 5、ほか(Python-2.0、WTFPL、0BSD、CC0 の併記など)各1 |
| tauri | MIT 31、Apache-2.0 22、Apache-2.0 OR MIT 13、MPL-2.0 12、ISC・BSD-3-Clause・0BSD 各1 |
| wails | MIT 32、Apache-2.0 22、MPL-2.0 12、ISC・BSD-3-Clause・0BSD 各1 |

MPL-2.0 は lightningcss(Vite が使うCSS処理)で、ビルド時だけ使う。配布物には入らない。

### 未確認

- Rust(Cargo)と Go の間接の依存。`Cargo.lock` と `go.sum` は CI の初回ビルドで生成される。
  UI技術を決めたあと、採用したものについて `cargo deny` や `go-licenses` などで確認する。
- Electron の配布物に入る Chromium、FFmpeg などの表記。Electron は `LICENSES.chromium.html` を同梱しており、
  FFmpeg は LGPL-2.1 でビルドされている。Electron を採用する場合は、表記の方法をあらためて確認する。
- WebView2 Runtime(Tauri と Wails が使う)は Windows の標準コンポーネントで、配布物には含めない。
