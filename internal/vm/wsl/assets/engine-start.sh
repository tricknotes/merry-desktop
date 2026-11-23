#!/bin/sh
# Starts Docker Engine. Run by WSL at distro boot ([boot] command in /etc/wsl.conf),
# so it must return quickly and leave dockerd running in the background.
set -eu

# The [boot] command runs with a minimal PATH; dockerd needs docker-proxy, runc and containerd from it.
export PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin

name=$(cat /etc/engine/name)
log=/var/log/dockerd.log

# WSL mounts cgroup v2 only when systemd is enabled; make sure it is there.
if ! mountpoint -q /sys/fs/cgroup; then
  mount -t cgroup2 cgroup2 /sys/fs/cgroup
fi

# Shared with every WSL2 distro of this user (but not exposed on the network).
install -d -m 0755 "/mnt/wsl/$name"

if pgrep -x dockerd >/dev/null 2>&1; then
  exit 0
fi

# shellcheck disable=SC1091
. /etc/engine/proxy.env
# /mnt/wsl outlives the distro, so a socket from the previous run can still be there.
rm -f "/mnt/wsl/$name/docker.sock"
setsid nohup /usr/local/bin/dockerd --config-file /etc/docker/daemon.json >>"$log" 2>&1 &

# Let non-root users of the other WSL2 distros use the shared socket. /mnt/wsl is only
# reachable from this Windows user's distros, which can already run as root here.
shared="/mnt/wsl/$name/docker.sock"
(
  i=0
  while [ "$i" -lt 120 ]; do
    if [ -S "$shared" ]; then chmod 0666 "$shared"; exit 0; fi
    sleep 0.5
    i=$((i + 1))
  done
) &
