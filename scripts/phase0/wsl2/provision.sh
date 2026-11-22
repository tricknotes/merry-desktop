#!/bin/sh
# Provisions the engine distro (Alpine) with Docker Engine from the static binaries.
# Runs inside the distro as root. Called by new-engine-distro.ps1.
#
# Environment:
#   ENGINE_NAME       distro name, also used for the shared socket dir (required)
#   DOCKER_VERSION    e.g. 29.10.0 (required)
#   COMPOSE_VERSION   e.g. v5.8.0 (required)
#   ENGINE_PROXY      optional proxy URL, e.g. http://proxy.example.com:8080
#   ENGINE_NO_PROXY   optional comma separated list
#   ENGINE_CA_DIR     optional directory with extra CA certificates (*.crt, PEM)
set -eu

: "${ENGINE_NAME:?}" "${DOCKER_VERSION:?}" "${COMPOSE_VERSION:?}"
script_dir=$(cd "$(dirname "$0")" && pwd)

log() { printf '[provision] %s\n' "$*"; }

# --- proxy and CA first, so that the downloads below go through them -------
sh "$script_dir/apply-network.sh"
# shellcheck disable=SC1091
. /etc/engine/proxy.env

log "installing packages"
apk update
apk add --no-cache ca-certificates curl iptables ip6tables socat
# The ca-certificates package rebuilt the bundle; add the extra certificates again.
sh "$script_dir/apply-network.sh" >/dev/null

arch=x86_64
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

log "installing Docker Engine $DOCKER_VERSION (static binaries)"
curl -fsSL -o "$tmp/docker.tgz" "https://download.docker.com/linux/static/stable/$arch/docker-$DOCKER_VERSION.tgz"
tar -xzf "$tmp/docker.tgz" -C "$tmp"
install -m 0755 "$tmp"/docker/* /usr/local/bin/

log "installing Docker Compose $COMPOSE_VERSION"
compose_url="https://github.com/docker/compose/releases/download/$COMPOSE_VERSION/docker-compose-linux-$arch"
curl -fsSL -o "$tmp/docker-compose" "$compose_url"
curl -fsSL -o "$tmp/docker-compose.sha256" "$compose_url.sha256"
(cd "$tmp" && sed 's/\*\{0,1\}docker-compose-linux-'"$arch"'/docker-compose/' docker-compose.sha256 | sha256sum -c -)
install -D -m 0755 "$tmp/docker-compose" /usr/local/lib/docker/cli-plugins/docker-compose

log "configuring the engine"
addgroup -S docker 2>/dev/null || true
install -d -m 0755 /etc/docker
cat > /etc/docker/daemon.json <<JSON
{
  "hosts": ["unix:///var/run/docker.sock", "unix:///mnt/wsl/$ENGINE_NAME/docker.sock"],
  "group": "docker",
  "log-driver": "local"
}
JSON
install -D -m 0755 "$script_dir/engine-start.sh" /usr/local/sbin/engine-start
install -D -m 0755 "$script_dir/apply-network.sh" /usr/local/sbin/engine-apply-network
printf '%s\n' "$ENGINE_NAME" > /etc/engine/name

cat > /etc/wsl.conf <<CONF
[boot]
command = /usr/local/sbin/engine-start

[user]
default = root

[interop]
# The engine distro does not need to run Windows programs.
appendWindowsPath = false
CONF

log "done"
