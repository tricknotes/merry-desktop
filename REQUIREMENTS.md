# 社内向けDocker実行環境アプリ 要件書

- 版: 2026-11-23
- 位置づけ: 本ファイルが仕様の正。変更するときは本ファイルを更新すること。
- 作業の進め方と制約は `CLAUDE.md` を参照。

## 1. 背景と目的

Docker Desktopのライセンス費用に依存せず、「入れたら動く」開発環境を社内に配布することが目的。

- Docker Desktopを無料で使えるのは、従業員250人未満かつ年間売上1,000万ドル未満の企業、個人利用、教育、非商用OSSに限られる([Docker公式](https://docs.docker.com/subscription-billing/desktop-license/))。
- 有償なのはDocker Desktopだけ。Docker EngineとMobyはApache 2.0で、利用制限はない([GCN](https://gcn.com/docker-desktop-ships-free-every-developer/21055/))。
- したがって本アプリはエンジンを作らない。Linux VMとDocker Engineを起動・管理する薄いGUIと、自社環境向けの初期設定を提供する。
- 現状、社内で使われているのはRancher DesktopかColimaのみ。Docker Desktopからの移行は想定しない。
- 本アプリは個人開発のプロジェクトで、ライセンスはMIT。

### ゴール

- インストーラー1つで、`docker` と `docker compose` が追加設定なしに動く。
- プロキシ、社内CA証明書、社内レジストリの設定を自動化し、問い合わせを減らす。
- 既存OSS(Rancher Desktop、Podman Desktop)にない、複数コンテナ開発向けの機能で差別化する。

### 非ゴール

- コンテナ・イメージの汎用的な一覧管理GUIの再実装(CLIと既存ツールで足りる)。
- Kubernetesクラスタの同梱(必要な人は kind などを別途利用)。
- Docker Desktop拡張機能との互換。
- Docker Desktopからのイメージ・ボリューム移行手順。

## 2. 対象ユーザーと前提

Windowsを先に実装し、同じコアにバックエンドを追加する形でmacOSに対応する。

| 項目 | 内容 |
| --- | --- |
| 対象OS(第1段) | Windows 11(WSL2が有効化できる端末) |
| 対象OS(第2段) | macOS(Apple Siliconのみ。Intel Macは対象外) |
| 主な利用者 | Webアプリ開発者。CLIとComposeが中心 |
| ヘビーユーザー | 複数リポジトリにまたがる10個以上のサービスを同時に動かす開発者 |
| 管理者 | 情シス・開発基盤担当。配布、設定統制、問い合わせ対応を行う |

### 前提

- Windowsでは、WSL2のディストリ内にDockerを入れればDocker Desktopは不要([DDEV](https://ddev.com/blog/docker-desktop-alternatives-arrive-for-ddev-colima/))。
- macOSではLimaをVM層に使う。Rancher DesktopとColimaもmacOSではLimaの上に作られている([DEV Community](https://dev.to/ericwoooo_kr/docker-is-not-the-only-option-anymore-what-i-actually-run-containers-with-in-2026-4e5o))。
- 利用者は `docker` コマンドをそのまま使う。既存のComposeファイルやスクリプトは変更しない。

## 3. 全体アーキテクチャ

OSに依存するのはVM操作だけに閉じ込め、UIとコアは両OSで共通にする。

```
┌──────────────────────────────────────────────────────┐
│ UI: トレイ常駐 + 設定・診断ウィンドウ                │
│ 起動・停止・状態表示 / セットアップウィザード / 診断 │
└──────────────────────────┬───────────────────────────┘
                           ▼
┌──────────────────────────────────────────────────────┐
│ 共通コア                                             │
│ 設定管理 / Compose連携 / ポート検査 / ディスク管理   │
│ / 自動更新                                           │
└──────────────────────────┬───────────────────────────┘
                           ▼
┌──────────────────────────────────────────────────────┐
│ VMバックエンド インターフェース                      │
│ 起動 / 停止 / コマンド実行 / 状態取得 / リソース設定 │
└────────────┬─────────────────────────────┬───────────┘
             ▼                             ▼
┌─────────────────────────┐   ┌─────────────────────────┐
│ Windows: WSL2専用       │   │ macOS: Lima VM          │
│ ディストリ(第1段)     │   │ (第2段で追加)         │
│ wsl.exe 経由で操作      │   │ limactl 経由で操作      │
└────────────┬────────────┘   └────────────┬────────────┘
             ▼                             ▼
┌──────────────────────────────────────────────────────┐
│ VM内: Docker Engine + Compose                        │
│ ホストの docker CLI がソケット経由で接続             │
└──────────────────────────────────────────────────────┘
```

Windows版でインターフェースを固めておけば、macOS対応はLima実装の追加だけで済む。

### 同梱・利用コンポーネント

| コンポーネント | 役割 | 備考 |
| --- | --- | --- |
| Docker Engine (Moby) | コンテナ実行 | VM内に導入。Apache 2.0 |
| Docker CLI / Compose / Buildx | 利用者が直接使うコマンド | ホスト側に配置。ライセンスは同梱前に要確認 |
| WSL2 | WindowsのVM層 | OS標準機能。専用ディストリをインポートして使う |
| Lima | macOSのVM層 | 第2段で同梱 |
| Trivy | イメージスキャン | 任意機能(I-02)。同梱せず、スキャンのときに公式イメージ(ダイジェスト固定)を VM 内で実行する。Apache 2.0 |

### UI実装技術(決定)

**Go + Wails v3** を使う(2026-11-23、フェーズ0の比較で決定)。

- フェーズ0で Tauri、Electron、Go + Wails の3候補を試作し、ビルド時間・配布物サイズ・常駐メモリを実測した。
- Wails は差分ビルドが約1.6秒(Tauri は約50秒)で、配布物は実行ファイル1つで約10MB(Electron は zip で約155MB)だった。
- Docker周辺のGoライブラリ(Engine APIクライアントなど)をそのまま使える。
- Wails v3 は2026年10月に正式版になったばかりのため、バージョンを固定して使う。
- 計測結果と比較の詳細は `docs/phase0-ui-comparison.md`。

## 4. 機能要件(必須)

必須機能は3群、計16件。P0が初回リリース必須、P1がその次。

### 4.1 コア(トレイ常駐)

| ID | 機能 | 要件 | 優先度 |
| --- | --- | --- | --- |
| F-01 | VMの起動・停止・再起動 | トレイメニューから1クリックで操作できる | P0 |
| F-02 | 状態表示 | 停止中・起動中・稼働中・異常をアイコンで区別する | P0 |
| F-03 | リソース設定 | CPU数、メモリ、ディスク上限をGUIで変更できる(Windowsでの扱いは下記) | P0 |
| F-04 | CLIの自動設定 | docker contextとPATHを自動で設定し、ターミナルから即利用できる | P0 |
| F-05 | ログイン時の自動起動 | オン/オフを選べる | P0 |
| F-06 | アイドル自動停止 | コンテナが0個の状態が一定時間続いたらVMを止め、次のCLI実行で復帰する | P1 |

F-03 の Windows での扱い:

- WSL2 の CPU数とメモリは、ディストリごとではなく WSL の VM 全体の設定(`%UserProfile%\.wslconfig` の `[wsl2]` の `processors` と `memory`)。利用者のほかのディストリにも効き、反映には WSL 全体の再起動(全ディストリの停止)が要る。
- 本アプリは `.wslconfig` の該当する2項目だけを書き換え、ほかの内容は変えない。書き換える前にバックアップを取る。
- 保存の前に、ほかのディストリにも効くことと、WSL 全体の再起動が要ることを確認画面で伝える。再起動は利用者が選んだときだけ行い、選ばなければ次に WSL が起動したときに反映される。
- ディスク上限は、専用ディストリのディスクの上限として設定する(`wsl --manage <ディストリ> --resize`)。ほかのディストリには影響しない。専用ディストリを次に起動する前に反映する。

F-06はDocker DesktopのResource Saverに相当する([Docker公式](https://docs.docker.com/desktop/settings-and-maintenance/settings/))。

### 4.2 社内配布・統制

| ID | 機能 | 要件 | 優先度 |
| --- | --- | --- | --- |
| F-10 | セットアップウィザード | WSL2の有効化確認から動作確認(hello-world)までを案内する | P0 |
| F-11 | プロキシ・社内CA証明書 | ホストの設定を読み取り、VMとDocker Engineに自動反映する。VPN の接続・切断などで設定が変わったら1分以内に検知して VM に反映する。エンジンは起動時に設定を読むため、動いているコンテナがなければ VM を再起動し、あれば再起動を促す通知を出す。ビルドのプロキシ(docker CLI の設定)を有効にしていれば、それも新しいプロキシに書き換える | P0 |
| F-12 | 社内レジストリ連携 | レジストリURLを初期登録し、ログインをGUIから行える | P0 |
| F-13 | 自動アップデート | アプリ本体とDocker Engineのバージョンを管理者が固定・配信できる | P0 |
| F-14 | 診断レポート | VM状態、ログ、ネットワーク疎通を1クリックでzip出力する。認証情報は除外する | P0 |
| F-15 | 管理者設定のロック | 配布プロファイルで指定した項目を利用者が変更できないようにする | P1 |

### 4.3 複数コンテナ開発向け

| ID | 機能 | 要件 | 優先度 |
| --- | --- | --- | --- |
| F-20 | Composeプロジェクト画面 | プロジェクト単位でサービスの状態とhealthcheckを一覧し、起動・停止できる | P1 |
| F-21 | 統合ログ | 全サービスのログを時系列でまとめ、サービス名と文字列で絞り込める | P1 |
| F-22 | ポート競合の検出 | 公開ポートを一覧し、ホスト側で使用中のポートとの衝突を警告する。作成時の警告は実装済み: コンテナ作成で公開するポートが Windows で使用中なら、使っているプロセスとともに、作成の応答の警告とアプリの通知で知らせる(作成は止めない)。公開ポートの一覧は未実装 | P1 |
| F-23 | ディスク管理 | イメージ・ビルドキャッシュ・ボリュームの使用量を表示し、種類別に削除できる。WindowsではVHDXの圧縮も行う。VM 内の整理は I-22 で実装済み。VHDX の回収は未決: WSL 2.6 では sparse VHD がデータ破損の恐れで無効(`--allow-unsafe` が要る)で、圧縮(Optimize-VHD、diskpart)は管理者権限が要る | P1 |

## 5. 機能要件(アイディアレベル)

採否未定の候補が18件。難易度と実現方法は設計段階の見立てで、試作による検証はしていない。
指示があるまで実装しないこと。

### 5.1 低コストで足せるもの

| ID | アイディア | 実現方法の見立て | 難易度 |
| --- | --- | --- | --- |
| I-01 | デバッグ用コンテナのアタッチ | 対象コンテナとPID・ネットワーク名前空間を共有するツール入りコンテナを起動する。シェルのないイメージも調べられる | 低 |
| I-02 | イメージ脆弱性スキャン(採用・実装済み) | 設定画面でVM内のイメージを選び、Trivyでスキャンして深刻度ごとの件数と一覧を表示する。Trivyは同梱せず、公式イメージ(ダイジェスト固定)をVM内で実行し、VMのプロキシとCA証明書を渡す。脆弱性データベースはボリュームに保存する | 低 |
| I-03 | ボリュームのバックアップ・復元 | 補助コンテナでtarに書き出し、読み込む | 低 |
| I-04 | docker contextの切替GUI | リモートホストや別VMをトレイから選ぶ | 低 |
| I-05 | 社内スターターテンプレート | よく使うCompose構成(DB、キャッシュなど)を雛形から生成する | 低 |
| I-06 | イメージの事前pull | 起動時に社内ベースイメージを更新しておく | 低 |
| I-07 | リソース使用量モニター(採用・実装済み) | `docker stats` をComposeプロジェクト単位で集計し、メモリ逼迫を通知する。設定画面に動いているコンテナの CPU とメモリをプロジェクトごとに表示し(Compose 以外のコンテナは1行にまとめる)、コンテナのメモリが VM のメモリの90%に達したら、いちばん使っているプロジェクトとともに通知する(80%を下回るまで再通知しない) | 低〜中 |
| I-08 | ネットワーク診断 | VPN、プロキシ、DNS、`host.docker.internal` の疎通を自動チェックし、対処法を表示する | 低〜中 |
| I-09 | 遅いバインドマウントの警告(採用・実装済み) | Windows のドライブ上のパス(`C:\`、`/mnt/c`)をバインドしたら、遅いことと、WSL のディストリに置けば速くなることを知らせる。`docker run` には作成の応答の警告で、Compose の利用者にはアプリの通知で伝える | 低 |
| I-22 | 未使用リソースの整理(採用・実装済み) | 消して困るかどうかで扱いを分ける。ビルドキャッシュ(30日使われていないもの)とタグのないイメージは、1日1回自動で消す(設定で止められる)。7日以上止まっているコンテナと、どのコンテナも使っていないイメージは候補として見せ、利用者が選んで消す。手元でビルドしたイメージ(pull したものでない)は目印を付ける。ボリュームはデータを失うので、容量と Compose プロジェクトと使用中かどうかを見せるだけで、消さない。Windows 側の空き容量の回収(VHDX)は F-23 で扱う | 低〜中 |

### 5.2 設計検討が必要なもの

| ID | アイディア | 実現方法の見立て | 難易度 |
| --- | --- | --- | --- |
| I-10 | 環境プロファイル | プロジェクトごとにCPU・メモリ割当を切り替える。WSL2ではVM再起動を伴う見込み | 中 |
| I-11 | 環境スナップショット | `wsl --export` / `--import` でVMを丸ごと保存・復元する。容量と所要時間が課題 | 中 |
| I-12 | 依存グラフ表示 | `docker compose config` の `depends_on` を図にし、起動順と障害箇所を示す | 中 |
| I-13 | ローカルドメイン | リバースプロキシを同梱し、`サービス名.localhost` でアクセスできるようにする | 中 |
| I-14 | Kubernetes YAML生成 | Compose定義から変換ツールで生成する | 中 |
| I-15 | 利用状況の収集(オプトイン) | 導入台数、バージョン分布、エラー率を管理者が把握する | 中 |
| I-16 | AIによるログ要約・設定診断 | 起動失敗時のログとCompose定義をLLMに渡して原因候補を出す。送信範囲は社内ポリシーに従う | 中 |

### 5.3 難易度が高いもの

| ID | アイディア | 実現方法の見立て | 難易度 |
| --- | --- | --- | --- |
| I-20 | 許可レジストリ制限 | pull元を社内指定のレジストリに限定する。認可プラグインかプロキシが必要になる見込み | 中〜高 |
| I-21 | 高速ファイル同期(macOS) | ホストとVM間を同期ツールで結び、bind mountの遅さを解消する | 高 |

### 既存ツールとの対応

- I-02、I-11、I-20はRancher Desktopにあり、Podman Desktopには見当たらない([スキャン](https://docs.rancherdesktop.io/1.6/getting-started/features/)、[スナップショット](https://www.suse.com/c/rancher_blog/rancher-desktop-1-11-introducing-snapshots-container-dashboard-and-more)、[許可イメージ](https://www.suse.com/c/rancher_blog/rancher-desktop-1-7-0-introducing-allowed-images-feature-and-more))。
- I-04とI-14はPodman Desktopが持つ機能([Red Hat](https://www.redhat.com/en/topics/containers/what-is-podman-desktop))。
- I-01はDocker Debug、I-21は同期ファイル共有に相当する([Dockerブログ](https://www.docker.com/blog/docker-desktop-4-27/))。
- WindowsではコードをWSL2内に置けばI-21は不要なので、macOS対応時に判断する。

## 6. 非機能要件

社内配布を前提に、管理者が無人で導入・更新できることを最優先にする。数値目標は仮置きで、技術検証後に確定する。

| 分類 | 要件 |
| --- | --- |
| 配布 | WindowsはMSIでサイレントインストールできる。macOSは署名・公証済みのpkgでMDM配布できる |
| 更新 | 社内の配信元から自動更新する。管理者がバージョンを固定・段階配信できる |
| 権限 | 日常利用に管理者権限を要求しない。必要なのは初回セットアップ時のみとする |
| セキュリティ | Dockerソケットをネットワークに公開しない。レジストリの認証情報はOSの資格情報ストアに保存する |
| 性能(仮) | VMのコールドスタートから `docker ps` 応答まで30秒以内。アプリ本体の常駐メモリは200MB以下 |
| 互換性 | 既存のComposeファイルと `docker` コマンドが無変更で動く。VS Code Dev Containersでの動作を確認する |
| 可観測性 | アプリとVMのログをローカルに保存し、診断レポート(F-14)に含める |
| ライセンス | 本アプリはMITライセンスとする。同梱物のライセンス表記をアプリ内に表示する。Docker Desktop由来のバイナリは一切含めない |
| 命名 | アプリ名とアイコンに「Docker」の名称・ロゴを使わない |
| アンインストール | VM、イメージ、設定を残すか削除するかを選べる |

## 7. リリース計画

Windowsで必須機能を一通り実装してからmacOSに広げる、5フェーズ構成。日程は未定。

1. **フェーズ0: 技術検証(Windows)**
    - 範囲: WSL2専用ディストリの自動構築、プロキシと社内CAの反映、VPN接続中の疎通、UI技術3候補の試作とビルド時間(初回・差分)の実測比較
    - 完了条件: 社内標準PCで `docker compose up` が通り、UI技術を1つに決める
2. **フェーズ1: Windows MVP**
    - 範囲: F-01〜F-05、F-10〜F-14
    - 完了条件: パイロットチームが本アプリだけで2週間業務を継続できる
3. **フェーズ2: Windows拡張**
    - 範囲: F-06、F-15、F-20〜F-23、5.1から採用したもの
    - 完了条件: Windows利用者全員に配布できる
4. **フェーズ3: macOS対応**
    - 範囲: Limaバックエンドの実装、署名・公証、MDM配布、I-21の要否判断
    - 完了条件: Windows版と同じ必須機能がmacOSで動く
5. **フェーズ4: アイディアの採否**
    - 範囲: 5.2と5.3から、利用者の要望と問い合わせ内容をもとに選定
    - 完了条件: 採用するものと見送るものを確定する

## 8. 決定事項・未決事項・リスク

### 決定済み

| 項目 | 決定 |
| --- | --- |
| Intel Mac | 対象外 |
| 本アプリのライセンス | MIT(個人開発のため。法務レビューは行わない) |
| 同梱物のライセンス表記 | 6章の要件として実装時に対応する |
| Docker Desktopからの移行手順 | 不要(社内はRancher DesktopかColimaのみ) |
| UI技術の決め方 | フェーズ0で3候補を試作し、ビルド時間を実測比較して決める |
| UI実装技術 | Go + Wails v3(2026-11-23決定) |

### 未決(着手前に決める)

- [ ] 社内VPN・セキュリティ製品とWSL2ネットワークの相性をどう検証するか
- [ ] 保守担当と問い合わせ窓口をどのチームが持つか

### 後回し

- 対象とするWindowsのバージョンと、WSL2有効化に必要な管理者権限を誰がどう付与するか。具体的な配布計画がまだないため、計画が固まった時点で決め、関連する実装もその後に行う。

### リスク

| リスク | 影響 | 対策 |
| --- | --- | --- |
| 自作の保守コストが既存ツール利用を上回る | 投資対効果が逆転する | フェーズ0終了時に、Rancher Desktop標準化+設定配布スクリプト案と比較して継続を判断する |
| WSL2やmacOSの更新でVM層が壊れる | 全利用者の開発が止まる | OS更新の先行検証と、アプリの段階配信・ロールバックを用意する |
| Rancher Desktop / Colima固有の挙動に依存したプロジェクトがある | 切替時に動かない | パイロットで主要リポジトリを棚卸しする |
| WSL2 のディストリはネットワーク名前空間を共有するため、ほかの Engine(利用者のディストリの dockerd、Rancher Desktop など)と iptables のチェインや `docker0` を取り合う | 片方の Engine が再起動すると、もう片方のコンテナが外に出られなくなる(開発中に再現した) | dockerd を専用のネットワーク名前空間で動かす(要検証)。それまでは、同時に動かさないよう案内する |
| DockerソケットへのアクセスがVM内のroot相当の権限になる | 端末内での権限昇格の足がかりになる | ソケットをローカルユーザーに限定し、rootlessモードの採用を検討する |

## 9. 参考情報

2026年11月22日時点の検索結果に基づく。ライセンス条件は変更されうるため、着手前に公式ページを再確認すること。

- [Docker Desktop license agreement (Docker公式)](https://docs.docker.com/subscription-billing/desktop-license/): 無料利用の条件
- [Docker FAQs (Docker公式)](https://www.docker.com/pricing/faq): 有償対象となる組織の例
- [GCNの解説記事](https://gcn.com/docker-desktop-ships-free-every-developer/21055/): Docker EngineとMobyがApache 2.0であること
- [Change your Docker Desktop settings (Docker公式)](https://docs.docker.com/desktop/settings-and-maintenance/settings/): Resource Saverの説明
- [Docker Desktop 4.27 (Dockerブログ)](https://www.docker.com/blog/docker-desktop-4-27/): 同期ファイル共有、Docker Debug
- [Rancher Desktop Features (公式ドキュメント)](https://docs.rancherdesktop.io/1.6/getting-started/features/): Trivyによるイメージスキャン
- [Rancher Desktop 1.11 (SUSE)](https://www.suse.com/c/rancher_blog/rancher-desktop-1-11-introducing-snapshots-container-dashboard-and-more): スナップショット
- [Rancher Desktop 1.7.0 (SUSE)](https://www.suse.com/c/rancher_blog/rancher-desktop-1-7-0-introducing-allowed-images-feature-and-more): 許可イメージ、MSIインストーラー
- [What is Podman Desktop? (Red Hat)](https://www.redhat.com/en/topics/containers/what-is-podman-desktop): Kubernetes YAML生成、複数エンジン管理
- [Docker Desktop alternatives for DDEV (DDEV)](https://ddev.com/blog/docker-desktop-alternatives-arrive-for-ddev-colima/): WSL2内にDockerを入れる構成
- [DEV Communityの比較記事](https://dev.to/ericwoooo_kr/docker-is-not-the-only-option-anymore-what-i-actually-run-containers-with-in-2026-4e5o): Rancher DesktopとColimaがLimaを使うこと
- Tauriのビルド時間の参考値: [noqta.tn](https://www.noqta.tn/en/blog/tauri-2-desktop-apps-rust-web-technologies-2026)(初回 Tauri 約80秒 / Electron 約16秒)、[dev.to](https://dev.to/ahonn/how-to-make-your-tauri-dev-faster-2en1)(Rust変更時の再コンパイル15〜60秒程度)。個人ブログの計測値。
