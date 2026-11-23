# dockerd を専用のネットワーク名前空間で動かす検証

WSL2 のディストリはネットワーク名前空間を共有している。そのため、本アプリの dockerd と、ほかの Engine(利用者のディストリの dockerd、Rancher Desktop など)が、iptables のチェインや `docker0` を取り合う。片方の Engine が再起動すると、もう片方のコンテナが外に出られなくなる(REQUIREMENTS.md 8章のリスク)。

対策の候補として、dockerd を専用のネットワーク名前空間(以下 netns)で動かせるかを検証した。

## 結論

**成立する。ただし、公開ポートを Windows とほかのディストリに見せる中継を自前で持つ必要がある。** 製品に入れるかどうかは、中継の設計(下記「製品に入れる場合の課題」)を詰めてから判断する。

| 確かめたこと | 結果 |
| --- | --- |
| netns の中で dockerd が起動し、イメージを pull できる | できる(`nginx:alpine` の pull は 4.6 秒) |
| netns の dockerd は、共有の netns の iptables を変えない | 変えない(変化 0 行。DOCKER 系のルール 18 行は netns の中だけにできた) |
| 同時に動くほかの dockerd(merry-dev-engine)に影響しない | 影響なし(netns の dockerd の稼働中に、merry-dev-engine のコンテナから HTTPS で通信できた) |
| 公開ポートに、共有の netns から netns のアドレスで届く | 届く(`10.231.0.2:18080` で 200) |
| 公開ポートに、Windows の `localhost` から届く | **届かない**(000)。WSL の localhost 転送は共有の netns の待ち受けしか見ない |
| 公開ポートに、共有の netns の `localhost` から届く | **届かない**。ほかのディストリからも同じ |
| 共有の netns に中継(`socat`)を置くと Windows から届く | 届く(`localhost:18081` → 中継 → netns で 200) |

## 構成

- netns `merrytest` と veth の組(共有側 `10.231.0.1/30`、netns 側 `10.231.0.2/30`)。netns の既定の経路は共有側。
- 共有の netns に、veth からの通信の MASQUERADE と FORWARD の許可を足す(ほかの dockerd が FORWARD の既定を DROP にするため)。
- DNS は、ディストリの `/etc/resolv.conf`(WSL の DNS、`10.255.255.254`)のままで名前解決できた。
- dockerd は `nsenter --net=/run/netns/merrytest` で起動する。**`ip netns exec` は使えない**: `/sys` をマウントし直すため cgroup が見えなくなり、dockerd が「Devices cgroup isn't mounted」で止まる。
- 検証は使い捨てのディストリ(Alpine 3.24 と apk の docker 29.9.3)で行い、終わったあとに共有の netns に足したルールと veth を消してディストリを削除した。

## 製品に入れる場合の課題

1. **公開ポートの中継**: engine-agent が Docker のイベントを見て、公開ポートごとに共有の netns で待ち受け、netns のアドレスへ中継する。TCP は単純だが、UDP、ポートの範囲、待ち受けアドレスの指定(`127.0.0.1:8080:80` など)への対応が要る。中継を通すと、コンテナから見た接続元はすべて中継のアドレスになる(docker-proxy と同じ)。
2. **共有の netns に足すルール**: MASQUERADE と FORWARD の許可は、ほかの dockerd が消すことはないが、利用者や別製品が iptables を初期化すると消える。起動時と定期的に確かめ直す必要がある。
3. **アドレスの衝突**: veth と `--bip` のアドレス帯が、社内ネットワークや VPN の経路と重ならないように選ぶ(設定可能にする)。
4. **作成時のポート競合チェック(F-22)**: 待ち受けるのが中継になるので、Windows 側の競合に加えて、共有の netns での競合(ほかの Engine の公開ポート)も中継の待ち受け失敗として検出できる。
5. **ホストへの通信**: `host.docker.internal` などでホストに届くかは未検証。

## 再現用のスクリプト

使い捨てのディストリで、root で実行する(`wsl -d <ディストリ> -u root --exec sh -s < spike.sh`)。iptables、iproute2、docker、socat、curl が要る。

```sh
#!/bin/sh
# Spike: run a dockerd in its own network namespace inside a WSL2 distro.
set -u
rm -f /tmp/ready /tmp/fwd-ready
cleanup() {
  docker -H unix:///run/mt-docker.sock rm -f web >/dev/null 2>&1
  [ -f /run/mt-docker.pid ] && kill "$(cat /run/mt-docker.pid)" 2>/dev/null; sleep 3
  pkill socat
  for ipt in iptables-legacy iptables-nft; do
    $ipt -t nat -D POSTROUTING -s 10.231.0.0/30 ! -o mt0 -m comment --comment merrytest -j MASQUERADE 2>/dev/null
    $ipt -D FORWARD -o mt0 -m conntrack --ctstate RELATED,ESTABLISHED -m comment --comment merrytest -j ACCEPT 2>/dev/null
    $ipt -D FORWARD -i mt0 -m comment --comment merrytest -j ACCEPT 2>/dev/null
  done
  ip link del mt0 2>/dev/null; ip netns del merrytest 2>/dev/null
  echo "== cleaned up"
}
trap cleanup EXIT
mountpoint -q /sys/fs/cgroup || mount -t cgroup2 cgroup2 /sys/fs/cgroup
ip link del mt0 2>/dev/null; ip netns del merrytest 2>/dev/null
snap() { { iptables-legacy-save 2>/dev/null; iptables-nft-save 2>/dev/null; } | grep -v '^#' | grep -v '^:' | sort; }
echo "== resolv: $(grep nameserver /etc/resolv.conf)"
snap > /tmp/ipt-before
mkdir -p /run/netns
ip netns add merrytest
ip link add mt0 type veth peer name mt1
ip link set mt1 netns merrytest
ip addr add 10.231.0.1/30 dev mt0 && ip link set mt0 up
ip netns exec merrytest sh -c 'ip link set lo up; ip addr add 10.231.0.2/30 dev mt1; ip link set mt1 up; ip route add default via 10.231.0.1'
echo "== ip_forward: $(cat /proc/sys/net/ipv4/ip_forward)"
# NAT and forwarding for the netns, in whichever iptables backend has the FORWARD DROP policy.
for ipt in iptables-legacy iptables-nft; do
  $ipt -t nat -A POSTROUTING -s 10.231.0.0/30 ! -o mt0 -m comment --comment merrytest -j MASQUERADE 2>/dev/null
  $ipt -I FORWARD -i mt0 -m comment --comment merrytest -j ACCEPT 2>/dev/null
  $ipt -I FORWARD -o mt0 -m conntrack --ctstate ESTABLISHED,RELATED -m comment --comment merrytest -j ACCEPT 2>/dev/null
done
snap > /tmp/ipt-mine
echo "== start dockerd in the netns"
# nsenter keeps the mounts (ip netns exec remounts /sys and hides the cgroups).
setsid nsenter --net=/run/netns/merrytest dockerd -H unix:///run/mt-docker.sock --data-root /var/lib/mt-docker \
  --exec-root /run/mt-docker --pidfile /run/mt-docker.pid --bip 10.232.0.1/24 >/var/log/mt-dockerd.log 2>&1 &
for i in $(seq 60); do [ -S /run/mt-docker.sock ] && docker -H unix:///run/mt-docker.sock info >/dev/null 2>&1 && break; sleep 1; done
export DOCKER_HOST=unix:///run/mt-docker.sock
docker version --format 'server {{.Server.Version}}'
snap > /tmp/ipt-after
echo "== root netns iptables changed by the netns dockerd (lines):"; diff /tmp/ipt-mine /tmp/ipt-after | grep -c '^[<>]'
echo "== netns iptables (DOCKER chains):"; ip netns exec merrytest iptables-save 2>/dev/null | grep -c DOCKER
echo "== pull"; time docker pull -q nginx:alpine
docker run -d --name web -p 18080:80 nginx:alpine
sleep 2
echo "== from the root netns via the netns address:"; curl -s -o /dev/null -w '%{http_code}\n' http://10.231.0.2:18080/
echo "== from the root netns via localhost:"; curl -s -m 3 -o /dev/null -w '%{http_code}\n' http://127.0.0.1:18080/ || echo fail
echo "== listeners in the root netns on 18080:"; ss -ltn | grep -c 18080
touch /tmp/ready
sleep 60
echo "== forwarder in the root netns: 0.0.0.0:18081 -> 10.231.0.2:18080"
setsid socat TCP-LISTEN:18081,fork,reuseaddr TCP:10.231.0.2:18080 >/dev/null 2>&1 &
sleep 1; curl -s -o /dev/null -w 'root ns 127.0.0.1:18081 -> %{http_code}\n' http://127.0.0.1:18081/
touch /tmp/fwd-ready
sleep 60
```
