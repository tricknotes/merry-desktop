# フェーズ0: Windows バックエンド(WSL2)の検証

- 状態: **検証済み(VPN 接続中の疎通と、Windows 側 `docker.exe` での確認は未検証)**
- 検証日: 2026-11-23
- 検証環境: Windows 11 Home(ビルド 26200)、WSL 2.6.1、カーネル 6.6.87.2。作業は WSL の Ubuntu 24.04 から行った
- スクリプト: `scripts/phase0/wsl2/`

## 結論

- WSL2 専用ディストリを、rootfs のダウンロードから Docker Engine の起動まで **約14秒** で自動構築できた。
- **完了条件の `docker compose up` が、このディストリの Engine 上で通った。**
  nginx と、healthcheck 付きの redis の2サービスを `--wait` 付きで約8秒で起動した(イメージの取得を含む)。
  公開ポートは Ubuntu 側からも Windows 側からも `localhost` で届いた。
- 別の WSL ディストリからはソケット共有で、Windows からは名前付きパイプの中継で、TCP を開かずに Engine につながる。
- プロキシと追加の CA 証明書を、ディストリ(apk、curl)と Docker Engine(`docker pull`)の両方に反映できた。
  TLS を検査するプロキシ(mitmproxy)を社内プロキシの代わりにして確かめた。
- コールドスタート(ディストリ停止状態から `docker ps` の応答まで)は **中央値 2.3 秒**(3回: 2.27 / 3.39 / 2.29 秒)。
  WSL の VM がすでに起動している状態での値。VM の起動から測った値は未計測。

## 構成

| 項目 | 選んだもの | 理由 |
| --- | --- | --- |
| ディストリのもと | Alpine Linux 3.24.4 の minirootfs(約3MB) | 小さく、起動が速い。SHA-256 を固定して検証する |
| Docker Engine | 公式の静的バイナリ 29.10.0(`download.docker.com/linux/static`) | ディストリに依存せず、版を固定できる(F-13 で管理者が版を固定する用途に合う) |
| Compose | v5.8.0(GitHub Releases、`.sha256` で検証) | CLI プラグインとして配置 |
| 起動 | `/etc/wsl.conf` の `[boot] command` で dockerd を起動 | Alpine に systemd がないため。起動は軽い |
| ソケット | `/var/run/docker.sock` と `/mnt/wsl/<名前>/docker.sock` | `/mnt/wsl` は同じ Windows ユーザーの WSL2 ディストリだけで共有される。TCP には出さない |
| Windows 側の接続 | 名前付きパイプ `\\.\pipe\<名前>-docker` の中継 | パイプはそのユーザーと SYSTEM だけが使える ACL にする |

ディストリ名は検証用に `phase0-engine` とした。アプリ名が決まったら、それに合わせる。

## 手順

### 構築

```powershell
powershell -ExecutionPolicy Bypass -File scripts\phase0\wsl2\new-engine-distro.ps1
```

`new-engine-distro.ps1` が行うこと:

1. Alpine の minirootfs をダウンロードし、SHA-256 を検証する。
2. `wsl --import` で WSL2 ディストリとして登録する(既定の場所は `%LOCALAPPDATA%\phase0-engine`)。
3. セットアップ用のファイルをディストリに送る(下記「ファイルの受け渡し」)。
4. ディストリの中で `provision.sh` を実行する。
   プロキシと CA の反映、パッケージ(ca-certificates、curl、iptables、socat)、Docker Engine、Compose、
   `daemon.json`、`/etc/wsl.conf` を順に設定する。
5. ディストリを再起動し、`[boot] command` で dockerd が起動して応答するまで待つ。

主な引数:

| 引数 | 意味 |
| --- | --- |
| `-Proxy` / `-NoProxy` | プロキシを明示する |
| `-UseHostProxy` | ホストの設定(環境変数、なければ「設定 > ネットワーク > プロキシ」の WinINET 設定)を読む |
| `-CaFile` | 追加する CA 証明書(PEM)。複数指定できる |
| `-ImportHostRootCerts` | ホストの「信頼されたルート証明機関」(ユーザーとコンピューター)をすべて取り込む |
| `-Force` | 同名のディストリを作り直す(中のイメージなどは消える) |

### 他の WSL ディストリから使う

```bash
docker context create phase0-engine --docker host=unix:///mnt/wsl/phase0-engine/docker.sock
docker --context phase0-engine compose up -d
```

Ubuntu で `docker context` を作り、`compose up` まで通ることを確かめた。

### Windows から使う

```powershell
powershell -ExecutionPolicy Bypass -File scripts\phase0\wsl2\pipe-relay.ps1
docker context create phase0-engine --docker host=npipe:////./pipe/phase0-engine-docker
```

`pipe-relay.ps1` は、接続ごとに `wsl.exe -d <名前> -u root --exec socat STDIO UNIX-CONNECT:/var/run/docker.sock`
を起動し、パイプとの間でデータを中継する。

確かめたのは、パイプ越しの Engine API(`/_ping`、`/version`、`/containers/json`)が HTTP 200 を返すことまで
(`test-pipe.ps1`)。Windows 版の `docker.exe` からの接続は未検証(下記)。

### プロキシと CA の反映

`apply-network.sh` が次を書き出す。何度実行しても、そのときの値で上書きする。

| 反映先 | 方法 |
| --- | --- |
| Docker Engine | `/etc/engine/proxy.env` を読んでから dockerd を起動する(`HTTP_PROXY` / `HTTPS_PROXY` / `NO_PROXY`) |
| ディストリ内のシェル | `/etc/profile.d/engine-proxy.sh` から同じファイルを読む |
| CA 証明書 | `/usr/local/share/ca-certificates/engine-*.crt` に置いて `update-ca-certificates` を実行する |

- `proxy.env` は URL に認証情報が入りうるため、root だけが読める権限(0600)にした。値はログに出さない。
- プロキシの値は `wsl.exe` の引数ではなく `WSLENV` で渡し、コマンドラインに出さないようにした。
- ホストの WinINET 設定から `NO_PROXY` への変換は、`host-network.tests.ps1` の5ケースで確かめた。
  `*.example.com` は `.example.com` に、`10.*` は `10.0.0.0/8` に変換する。
  `<local>`(ドット無しのホスト名を直接つなぐ)は、`NO_PROXY` に対応する書き方がないため、`localhost` だけにしている。
- PAC ファイル(`AutoConfigURL`)は評価しない。設定されていれば警告を出し、`-Proxy` での指定を求める。

検証方法: Engine の上に mitmproxy を立て、その CA を `-CaFile` に渡して2台目のディストリ(`phase0-proxytest`)を構築した。

- CA なしで mitmproxy を通すと、curl は証明書エラーになる(社内の TLS 検査プロキシと同じ状態)。
- 構築中の apk、Docker と Compose のダウンロードがすべて mitmproxy を通り、成功した。
- 2台目の Engine で `docker pull alpine:3.24` が mitmproxy を通り(`registry-1.docker.io`、`auth.docker.io` などへの要求を確認)、コンテナも起動した。

## 検証中にわかったこと

フェーズ1の設計に効く点。

1. **WSL は、`wsl.exe` のセッションがないディストリをしばらくすると止める。**
   `[boot] command` で起動した dockerd も、ディストリごと止まる(ログに終了の記録は残らない)。
   `docker compose up` の途中で接続が切れたことで気づいた。
   → アプリが Engine を動かしている間は、`wsl.exe -d <名前> --exec sleep infinity` のようなセッションを1本保つ。
   F-06(アイドル自動停止)は、このセッションを切ることで実現できる見込み。
2. **`wsl.exe` のセッション内で起動したプロセスは、セッションが終わると止まる。**
   → dockerd は `setsid` で切り離して起動する。手動での再起動も、ディストリの停止と起動で行うのが確実。
3. **`/mnt/wsl` はディストリより長く残る。**
   前回のソケットが残っているので、起動時に消してから dockerd を起動する。
4. **共有ソケットの権限は 0666 にした。**
   別のディストリの一般ユーザーは、Engine 側の `docker` グループに入っていないため、0660 ではつながらない。
   `/mnt/wsl` に届くのは同じ Windows ユーザーの WSL ディストリだけで、そのユーザーはもともと
   `wsl -u root` で Engine 側に入れる。権限を絞っても守れるものがないため、こうした。
5. **`[boot] command` は PATH が最小限で動く。** dockerd が `docker-proxy` などを見つけられずに起動に失敗したため、PATH を明示した。
6. **ディストリへのファイルの受け渡しは、`/mnt/c` に頼らない。**
   `wsl.exe` の引数に base64 で載せて送る(`Copy-ToDistro`)。社内PCでは、ポリシーで `/mnt/c` の自動マウントが切られていることがある。
   この検証環境でも、PowerShell が書いたファイルが仮想化されて `/mnt/c` から見えなかった。
7. **名前付きパイプの中継は、接続ごとに `wsl.exe` を起動するため1要求に約0.7秒かかる。**
   `docker` CLI は1コマンドで複数回接続するため、体感で遅くなる。
   → フェーズ1で Go に実装するときは、常時つないでおく経路(AF_VSOCK / Hyper-V ソケットなど)を検討する。
8. **Smart App Control は、自分で書いた PowerShell スクリプトは止めなかった。** 署名のない exe は止める(フェーズ0の 0-2 で確認)。

## 未検証のこと

- **VPN 接続中の疎通**(社内環境が必要)。下記の手順で確認をお願いしたい。
- **Windows 版 `docker.exe` からの接続**。この作業環境では、外部の exe をダウンロードして動かす操作が許可されなかった。
  パイプ越しの Engine API の応答までは確かめた。手順は下記。
- ホストのプロキシ設定を実際に読んだ場合の反映。作業用PCにはプロキシの設定がないため、変換部分だけをテストした。
- `-ImportHostRootCerts` での大量の証明書(数十件)の取り込み。
- WSL の VM 自体が止まっているところからのコールドスタート時間。
- コンテナ内のプロセスへのプロキシの反映。今回反映したのは Engine(pull)とディストリまで。
  コンテナに渡すには、利用者の `~/.docker/config.json` の `proxies` か、`docker run -e` が必要になる。フェーズ1で扱いを決める。
- 管理者権限が要る操作。この手順は、WSL2 が有効になっていれば一般ユーザーで動いた。WSL2 の有効化自体は後回しの決定どおり扱っていない。

## 人間に依頼したい確認

### 1. Windows 版 docker CLI からの接続

```powershell
# 1. https://download.docker.com/win/static/stable/x86_64/docker-29.10.0.zip を展開し、docker.exe に PATH を通す
# 2. 中継を起動したまま、別の PowerShell で:
docker context create phase0-engine --docker host=npipe:////./pipe/phase0-engine-docker
docker --context phase0-engine version
docker --context phase0-engine run --rm hello-world
```

Engine が止まっていたら、`wsl.exe -d phase0-engine --exec sleep infinity` を別のウィンドウで動かしておく。

### 2. VPN 接続中の疎通

社内VPNにつないだ状態で、次を順に確かめる。結果(成功/失敗、エラーメッセージ)を `docs/phase0-wsl2-backend.md` に追記する。

| # | 確認 | コマンド例 |
| --- | --- | --- |
| 1 | 社内プロキシと CA を指定して構築できる | `new-engine-distro.ps1 -Name phase0-vpn -UseHostProxy -ImportHostRootCerts` |
| 2 | 社外のレジストリから pull できる | `wsl -d phase0-vpn --exec docker pull alpine` |
| 3 | 社内レジストリから pull できる | `wsl -d phase0-vpn --exec docker pull <社内レジストリ>/<イメージ>` |
| 4 | コンテナから社内のホスト名を引ける | `wsl -d phase0-vpn --exec docker run --rm alpine nslookup <社内ホスト>` |
| 5 | コンテナから社内のサービスにつながる | `wsl -d phase0-vpn --exec docker run --rm alpine wget -qO- http://<社内ホスト>/` |
| 6 | 大きなデータが途中で止まらない(MTU の問題の確認) | 数百MB のイメージを pull する |
| 7 | VPN をつないだまま `docker compose up` が通る | 任意の compose プロジェクトで |

うまくいかない場合は、`%USERPROFILE%\.wslconfig` の `[wsl2] networkingMode=mirrored` でも試してほしい。
VPN の製品によっては、WSL2 の既定の NAT モードで社内につながらないことが知られている(未確認)。

## 後片付け

```powershell
wsl --unregister phase0-engine
wsl --unregister phase0-proxytest
Remove-Item -Recurse "$env:LOCALAPPDATA\phase0-engine"
```
