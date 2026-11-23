# Build environment for local development and CI.
# Cross-compiles the Windows app from Linux, so no toolchain is installed on the host.
ARG GO_VERSION=1.27.4
ARG NODE_VERSION=26.13.1

FROM node:${NODE_VERSION}-trixie-slim AS node

FROM golang:${GO_VERSION}-trixie
COPY --from=node /usr/local/bin/node /usr/local/bin/node
COPY --from=node /usr/local/lib/node_modules /usr/local/lib/node_modules
RUN ln -s ../lib/node_modules/npm/bin/npm-cli.js /usr/local/bin/npm \
 && ln -s ../lib/node_modules/npm/bin/npx-cli.js /usr/local/bin/npx \
 && apt-get update \
 && apt-get install -y --no-install-recommends shellcheck \
 && rm -rf /var/lib/apt/lists/* \
 # Caches live in named volumes; make them writable for any uid the container runs as.
 && mkdir -p /cache/go-mod /cache/go-build /cache/npm /tmp/home \
 && chmod -R 1777 /cache /tmp/home

ENV GOMODCACHE=/cache/go-mod \
    GOCACHE=/cache/go-build \
    npm_config_cache=/cache/npm \
    HOME=/tmp/home \
    GOFLAGS=-buildvcs=false
WORKDIR /src
