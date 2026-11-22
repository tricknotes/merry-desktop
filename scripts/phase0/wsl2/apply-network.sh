#!/bin/sh
# Applies proxy and extra CA certificates to the distro and to Docker Engine.
# Safe to run again: it rewrites the generated files from the current values.
#
# Environment (all optional):
#   ENGINE_PROXY      proxy URL used for HTTP and HTTPS
#   ENGINE_NO_PROXY   comma separated hosts that bypass the proxy
#   ENGINE_CA_DIR     directory with extra CA certificates (*.crt, PEM)
set -eu

proxy=${ENGINE_PROXY:-}
no_proxy=${ENGINE_NO_PROXY:-localhost,127.0.0.1,::1}

install -d -m 0755 /etc/engine
umask 077
# proxy.env may contain credentials in the URL; keep it root-only and never log it.
{
  if [ -n "$proxy" ]; then
    printf 'export HTTP_PROXY=%s\nexport HTTPS_PROXY=%s\nexport NO_PROXY=%s\n' "$proxy" "$proxy" "$no_proxy"
    printf 'export http_proxy=%s\nexport https_proxy=%s\nexport no_proxy=%s\n' "$proxy" "$proxy" "$no_proxy"
  fi
} > /etc/engine/proxy.env
umask 022
# Shells inside the distro pick up the same settings.
printf '. /etc/engine/proxy.env\n' > /etc/profile.d/engine-proxy.sh

# update-ca-certificates only reads the top level of /usr/local/share/ca-certificates,
# so the extra certificates go there with an "engine-" prefix.
rm -f /usr/local/share/ca-certificates/engine-*.crt
if [ -n "${ENGINE_CA_DIR:-}" ] && ls "$ENGINE_CA_DIR"/*.crt >/dev/null 2>&1; then
  install -d -m 0755 /usr/local/share/ca-certificates
  for crt in "$ENGINE_CA_DIR"/*.crt; do
    cp "$crt" "/usr/local/share/ca-certificates/engine-$(basename "$crt")"
  done
  echo "[network] installed $(ls "$ENGINE_CA_DIR"/*.crt | wc -l) extra CA certificate(s)"
fi
if command -v update-ca-certificates >/dev/null 2>&1; then
  update-ca-certificates >/dev/null 2>&1 || true
elif ls /usr/local/share/ca-certificates/engine-*.crt >/dev/null 2>&1; then
  # Before the ca-certificates package is installed: append to the bundle that apk uses.
  cat /usr/local/share/ca-certificates/engine-*.crt >> /etc/ssl/certs/ca-certificates.crt
fi

if [ -n "$proxy" ]; then
  echo "[network] proxy configured (value not shown)"
else
  echo "[network] no proxy"
fi
