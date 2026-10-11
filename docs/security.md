# Security

## No telemetry

citric sends no analytics, crash reports or update checks, and loads nothing
from CDNs. Its only outbound connections are ones you configure:

| From | To | When |
| --- | --- | --- |
| Server | your OIDC provider (discovery, keys, token exchange) | SSO is configured |
| Server | your Keycloak admin API | Keycloak sync is configured |
| Server | itself on `localhost` (git hooks) | on push |
| CLI | your citric server, including `citric update` from `/dl` | always |
| Build node | your citric server | always |
| Web UI | the same origin only (fonts are bundled) | always |

Build commands can of course reach whatever the network allows; that's your
CI, not citric. CI checks that the server starts and passes its health check
with no network at all.

## Access model

- Reading open repositories (clone, fetch, browse) is allowed without signing
  in. Mark a repository restricted to limit it to allowed users and roles.
- Pushing always requires a signed-in user, on every repository: a push opens
  a CL, and a CL runs that commit's build commands.
- Build commands run on the server (or a node) with the service account's
  permissions. Run untrusted contributors' CLs on [nodes](nodes.md) with
  `CITRIC_LOCAL_BUILDS=false`.
- Secrets at rest: the JWT signing key and the git hook token live in
  `<data_dir>` with mode 0600; node credentials and join tokens are stored
  hashed.

## Reporting a vulnerability

See [SECURITY.md](../SECURITY.md).
