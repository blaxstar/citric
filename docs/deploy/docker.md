# Docker

| Image | Contents |
| --- | --- |
| `ghcr.io/blaxstar/citric:<version>`, `:latest` | git + CitriC (~450 MB) |
| `ghcr.io/blaxstar/citric:<version>-full`, `:latest-full` | + Go, Node LTS, Rust, Python/uv, shellcheck, golangci-lint, biome (~2.4 GB) |

Both are multi-arch (`linux/amd64`, `linux/arm64`) and run as an unprivileged
user (uid 10001).

**Builds run inside the server container**, so the image decides which
toolchains CI has. Use `-full`, or extend the slim image with yours:

```dockerfile
FROM ghcr.io/blaxstar/citric:latest
USER root
RUN apt-get update && apt-get install -y --no-install-recommends openjdk-21-jdk-headless
USER citric
```

## Compose files

- [`compose.yaml`](../../compose.yaml): the quickstart.
- [`compose.caddy.yaml`](../../compose.caddy.yaml): CitriC behind Caddy with
  automatic HTTPS: `CITRIC_DOMAIN=citric.example.com docker compose -f compose.caddy.yaml up -d`.
- [`compose.nodes.yaml`](../../compose.nodes.yaml): the server plus two
  [build nodes](../nodes.md).

## Volumes

| Path | Holds |
| --- | --- |
| `/var/lib/citric` | `data/` (database, logs, secrets) and `repos/` (bare git repos) — back this up |
| `/var/cache/citric` | build caches; safe to delete |

## Upgrading

```sh
docker compose pull && docker compose up -d
```

The database migrates itself on start. Take a [backup](../backups.md) first.

## Health

`/healthz` returns 200 when the server can reach its database; the image's
`HEALTHCHECK` runs `citric healthcheck`. Containers running `citric node run`
have no server to probe: start them with `--no-healthcheck`
(compose: `healthcheck: { disable: true }`).
