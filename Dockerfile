# syntax=docker/dockerfile:1
#
# Two images from one file:
#   docker build --target slim -t citric .   git + citric: review, or extend with your own toolchains
#   docker build --target full -t citric .   + Go, Node, Rust, Python/uv, shellcheck, golangci-lint, biome
#
# Multi-arch: docker buildx build --platform linux/amd64,linux/arm64 ...

ARG GO_VERSION=1.25
ARG NODE_VERSION=22
# BuildKit sets these; the defaults keep the legacy builder working.
ARG BUILDPLATFORM=linux/amd64
ARG TARGETARCH=amd64

# ── web UI ────────────────────────────────────────────────────────────────────
FROM --platform=$BUILDPLATFORM node:${NODE_VERSION}-bookworm-slim AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

# ── server + CLI binaries ─────────────────────────────────────────────────────
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-bookworm AS build
ARG TARGETOS TARGETARCH
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/internal/web/dist ./internal/web/dist
ENV CGO_ENABLED=0
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" \
      -o /out/target/citric ./cmd/citric
# The clients /dl and install.sh / install.ps1 hand out.
RUN for t in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64; do \
      os=${t%/*}; arch=${t#*/}; ext=; [ "$os" = windows ] && ext=.exe; \
      GOOS=$os GOARCH=$arch go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" \
        -o /out/dist/citric-$os-$arch$ext ./cmd/citric || exit 1; \
    done

# ── slim: git + citric ────────────────────────────────────────────────────────
FROM debian:bookworm-slim AS slim
RUN apt-get update \
 && apt-get install -y --no-install-recommends git ca-certificates curl tini \
 && rm -rf /var/lib/apt/lists/* \
 && useradd --uid 10001 --create-home --home-dir /home/citric --shell /usr/sbin/nologin citric \
 && mkdir -p /var/lib/citric /var/cache/citric /var/lib/citric-node \
 && chown citric:citric /var/lib/citric /var/cache/citric /var/lib/citric-node
COPY --from=build /out/target/citric /usr/local/bin/citric
COPY --from=build /out/dist /usr/share/citric/dist
COPY LICENSE NOTICE /usr/share/doc/citric/
ENV CITRIC_DATA_DIR=/var/lib/citric/data \
    CITRIC_REPOS_DIR=/var/lib/citric/repos \
    CITRIC_CACHE_DIR=/var/cache/citric \
    CITRIC_NODE_DATA_DIR=/var/lib/citric-node
USER citric
WORKDIR /home/citric
EXPOSE 4400
VOLUME ["/var/lib/citric", "/var/cache/citric"]
# For a build node (citric node run) there is no server to probe; run nodes
# with --no-healthcheck (compose.nodes.yaml does).
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s CMD ["citric", "healthcheck"]
ENTRYPOINT ["tini", "--", "citric"]
CMD ["serve"]

# ── full: slim + the toolchains every `citric template` project type needs ───
FROM golang:${GO_VERSION}-bookworm AS gotool
FROM node:${NODE_VERSION}-bookworm-slim AS nodetool
FROM ghcr.io/astral-sh/uv:0.8.17 AS uvtool

FROM slim AS full
ARG TARGETARCH
ARG RUST_VERSION=1.90.0
ARG GOLANGCI_LINT_VERSION=2.5.0
ARG BIOME_VERSION=2.5.3
USER root
RUN apt-get update \
 && apt-get install -y --no-install-recommends build-essential pkg-config python3 python3-venv \
      shellcheck jq xz-utils unzip \
 && rm -rf /var/lib/apt/lists/*
COPY --from=gotool /usr/local/go /usr/local/go
COPY --from=nodetool /usr/local/ /usr/local/
COPY --from=uvtool /uv /uvx /usr/local/bin/
# Toolchains land on the default build PATH (/usr/local/bin): builds run with a
# throwaway HOME, so nothing may depend on a per-user install (no rustup).
RUN set -eux; \
    ln -s /usr/local/go/bin/go /usr/local/go/bin/gofmt /usr/local/bin/; \
    case "$TARGETARCH" in amd64) rarch=x86_64 ;; arm64) rarch=aarch64 ;; *) exit 1 ;; esac; \
    curl -fsSL "https://static.rust-lang.org/dist/rust-${RUST_VERSION}-${rarch}-unknown-linux-gnu.tar.xz" | tar -xJ -C /tmp; \
    /tmp/rust-${RUST_VERSION}-${rarch}-unknown-linux-gnu/install.sh --prefix=/usr/local \
      --components=rustc,cargo,rust-std-${rarch}-unknown-linux-gnu,clippy-preview,rustfmt-preview; \
    rm -rf /tmp/rust-*; \
    curl -fsSL "https://github.com/golangci/golangci-lint/releases/download/v${GOLANGCI_LINT_VERSION}/golangci-lint-${GOLANGCI_LINT_VERSION}-linux-${TARGETARCH}.tar.gz" \
      | tar -xz -C /usr/local/bin --strip-components=1 "golangci-lint-${GOLANGCI_LINT_VERSION}-linux-${TARGETARCH}/golangci-lint"; \
    npm install -g --no-audit --no-fund "@biomejs/biome@${BIOME_VERSION}"; \
    npm cache clean --force; \
    go version; node --version; cargo --version; uv --version; golangci-lint --version; biome --version
USER citric
