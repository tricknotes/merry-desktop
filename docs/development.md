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
| `make lint` | シェルスクリプトの shellcheck |
| `make test` | Go の `vet`(Windows 向け) |
| `make build` | Windows 向けの実行ファイルを作る |
| `make clean` | ビルド結果を消す |

Go のモジュール、ビルドキャッシュ、npm のキャッシュは名前付きボリュームに残る。
キャッシュのない状態で約28秒、2回目以降のビルドは約2.5秒だった(2026-11-23、`phase0-engine` 上)。

CI(`.github/workflows/build.yml`)も同じイメージと Makefile を使う。

## Engine が別の WSL ディストリにある場合

`compose.yaml` は、このチェックアウトを `/src` にバインドマウントする。
バインドマウントのパスは Engine 側で解決されるため、Engine が別の WSL ディストリにあると
(フェーズ0の `phase0-engine` など)、`/src` が空になる。

そのときは、チェックアウトを `/mnt/wsl`(すべての WSL2 ディストリで共有される)にバインドマウントし、
`DEV_SRC` でその場所を渡す。

```bash
sudo mkdir -p /mnt/wsl/phase0-share/merry-desktop
sudo mount --bind "$PWD" /mnt/wsl/phase0-share/merry-desktop   # WSL を再起動すると消える
export DOCKER_HOST=unix:///mnt/wsl/phase0-engine/docker.sock
export DEV_SRC=/mnt/wsl/phase0-share/merry-desktop
docker compose run --rm builder make all
```

`phase0-engine` は、`wsl.exe` のセッションがないと WSL に止められる。使う間は別のターミナルで
`wsl.exe -d phase0-engine -u root --exec sleep infinity` を動かしておく。

Rancher Desktop や Colima など、利用者の環境と同じディストリから見える Engine を使う場合は、`DEV_SRC` はいらない。

## 動作確認

作業用PCは Smart App Control で署名のない exe を起動できないため、動作確認は CI で行う(2026-11-23 決定)。
実機での確認は、別のPCで行う。
