# Configuration

Every setting has a `CITRIC_*` environment variable, and a TOML file is
optional: citric starts on defaults alone. Precedence is environment > file >
default. The full annotated file is
[`config/citric.toml.example`](../config/citric.toml.example); the server reads
`/etc/citric/citric.toml` (or `--config <path>`).

Secrets (`CITRIC_JWT_SECRET`, `CITRIC_OIDC_CLIENT_SECRET`) also accept a
`_FILE` variant naming a file to read, for Docker and Kubernetes secrets.

## Server

| Setting | Env var | Default |
| --- | --- | --- |
| `server.public_url` | `CITRIC_PUBLIC_URL` | the request's host; set it behind a proxy |
| `server.host` / `server.port` | `CITRIC_HOST` / `CITRIC_PORT` | `0.0.0.0` / `4400` |
| `server.data_dir` | `CITRIC_DATA_DIR` | `/var/lib/citric/data` (Windows: `%ProgramData%\citric\data`) |
| `server.repos_dir` | `CITRIC_REPOS_DIR` | `/var/lib/citric/repos` |
| `server.cache_dir` | `CITRIC_CACHE_DIR` | `<data_dir>/cache` |
| `server.max_concurrent` | `CITRIC_MAX_CONCURRENT` | `4` builds at once on the server |
| `server.max_concurrent_per_cl` | `CITRIC_MAX_CONCURRENT_PER_CL` | `2` |
| `server.queue_mode` | `CITRIC_QUEUE_MODE` | `false`; `true` builds one CL at a time |
| `server.build_path` | `CITRIC_BUILD_PATH` | standard system PATH; builds don't inherit the server's |
| `server.local_builds` | `CITRIC_LOCAL_BUILDS` | `true`; `false` sends every build to [nodes](nodes.md) |
| `server.local_labels` | `CITRIC_LOCAL_LABELS` | extra `runs_on` labels the server offers |
| `server.dist_dir` | `CITRIC_DIST_DIR` | `/usr/share/citric/dist`, client binaries for `/dl` |
| `ui.title` | `CITRIC_SITE_TITLE` | `citric` |
| `ui.theme` | `CITRIC_THEME` | `tangerine-dusk`; the default color palette (`navel`, `meyer-lemon`, `blood-orange`, `grapefruit`, `key-lime`, `yuzu`, `marmalade`, `zest`, `clementine-sea`). People can pick their own under Appearance. |

## Auth

| Setting | Env var | Default |
| --- | --- | --- |
| `auth.jwt_secret` | `CITRIC_JWT_SECRET(_FILE)` | generated into `<data_dir>/jwt_secret` on first start |
| `auth.token_ttl` | `CITRIC_TOKEN_TTL` | `720h` |
| | `CITRIC_ADMIN_USER` | on a server with no admin yet, create this one and log a CLI token |

Without single sign-on, people sign in with CLI tokens. An admin creates the
first account with `CITRIC_ADMIN_USER`, or on the server with
`citric admin create <name>`.

## Single sign-on (OIDC)

Works with Keycloak, Authentik, Authelia, Zitadel, Okta, Entra ID, Google and
other OpenID Connect providers. Register citric as a confidential client with
the redirect URI `<public_url>/auth/callback`, the authorization-code flow and
PKCE.

| Setting | Env var | |
| --- | --- | --- |
| `oidc.issuer` | `CITRIC_OIDC_ISSUER` | discovery from `<issuer>/.well-known/openid-configuration` |
| `oidc.client_id` | `CITRIC_OIDC_CLIENT_ID` | |
| `oidc.client_secret` | `CITRIC_OIDC_CLIENT_SECRET(_FILE)` | |
| `oidc.admin_role` | `CITRIC_OIDC_ADMIN_ROLE` | `citric-admin`; holders become admins |
| `oidc.roles_claim` | `CITRIC_OIDC_ROLES_CLAIM` | an extra claim to read roles from (dot path) |

Roles are read from Keycloak's `realm_access` / `resource_access`, from
`roles` and `groups`, and from `roles_claim`. Usernames come from
`preferred_username`, else the email's local part.

**Keycloak user sync** (optional) imports every realm user as a reviewer and
keeps roles current through the admin API: set `oidc.url` and `oidc.realm`
(`CITRIC_KEYCLOAK_URL`, `CITRIC_KEYCLOAK_REALM`) and give the client a service
account with `view-users`. `CITRIC_KEYCLOAK_SYNC=false` keeps SSO but turns
sync off.

## Build tools

The server defines the tool names and how they gate submission; each repo
supplies commands in `.citric.toml` at its root (`citric template --write`
generates one).

```toml
[[tool]]
name    = "lint"                 # a tool the server knows: lint, test, integration, format
command = "golangci-lint run ./..."
timeout = "5m"                   # optional
shell   = "bash"                 # optional: sh (default), bash, pwsh, powershell, cmd
runs_on = ["linux"]              # optional: executor labels, see docs/nodes.md
[tool.env]
GOFLAGS = "-mod=readonly"
```

A tool a repo doesn't define is skipped, not failed, so adding a required tool
on the server never breaks existing repos. Commands run in a fresh checkout
with `CITRIC_CL_ID`, `CITRIC_PS_NUMBER`, `CITRIC_COMMIT_SHA`, `CITRIC_REPO`
and `CITRIC_TARGET_BRANCH` set, and a throwaway `HOME`.

Without `[[build_tool]]` entries in the server config, citric uses lint
(required), test, integration and format.
