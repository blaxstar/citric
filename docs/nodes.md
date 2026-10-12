# Build nodes

A build node is another machine that runs build tools for a CitriC server, to
add capacity or another platform. Without any nodes, the server runs every
build itself, and nothing about this page applies.

- **Outbound only.** A node long-polls the server for work and streams logs
  back; the server never connects to a node. Nodes work from behind NAT and
  need no open ports.
- **Labels.** Every executor offers its OS and architecture (`linux`, `amd64`,
  `windows`, `arm64`, …) plus any labels you give it. A tool with
  `runs_on = ["windows"]` in `.citric.toml` only runs where those labels are.
- **The server is node zero.** It keeps running builds it has the labels for.
  Set `CITRIC_LOCAL_BUILDS=false` to send everything to nodes, so code from a
  CL never runs on the server itself.

## Adding a node

As an admin, create a one-time join token (valid 24 hours), in the web UI
under **Admin → Build nodes → Add node**, or:

```sh
citric node token create
```

On the new machine, with the CitriC binary installed:

```sh
citric node join --server https://citric.example.com --token <token> [--name <name>]
citric node run --labels gpu --max-concurrent 4
```

To run it as a service (systemd, launchd or a Windows service), join as the
account the service will use, then install it (`--node-config` names the file
`join` wrote, if it's not the default):

```sh
sudo citric node join --server https://citric.example.com --token <token>
sudo citric node install-service
```

With Docker (the `-full` image has the common toolchains):

```sh
docker run -d --name citric-node --no-healthcheck \
  -e CITRIC_NODE_SERVER=https://citric.example.com \
  -e CITRIC_NODE_JOIN_TOKEN=<token> \
  -v citric-node:/var/lib/citric-node \
  ghcr.io/blaxstar/citric:latest-full node run
```

The container joins on first start and keeps its credential in the volume.

| `citric node run` flag | Env var | Default |
| --- | --- | --- |
| `--labels` | `CITRIC_NODE_LABELS` | none (OS and arch are always offered) |
| `--max-concurrent` | `CITRIC_NODE_MAX_CONCURRENT` | half the CPUs |
| `--data-dir` | `CITRIC_NODE_DATA_DIR` | `/var/lib/citric-node` as root, else the user cache dir |
| `--build-path` | `CITRIC_NODE_BUILD_PATH` | the standard system PATH |
| | `CITRIC_NODE_SERVER`, `CITRIC_NODE_NAME`, `CITRIC_NODE_JOIN_TOKEN` | |

## Routing tools

```toml
# .citric.toml
[[tool]]
name    = "test"
command = "go test ./..."           # runs anywhere

[[tool]]
name    = "integration"
command = "./scripts/windows-integration.ps1"
runs_on = ["windows"]               # only on Windows nodes
```

A tool whose labels no online executor has waits in the queue (its log says
so) until one joins, or until it's cancelled.

## Managing nodes

```sh
citric node list
citric node drain <name>     # finish current builds, take no new ones
citric node resume <name>
citric node remove <name>    # its credential stops working immediately
```

A node that stops reporting for 60 seconds has its running builds failed with
`[node lost]`.

## What a node can access

A node's credential lets it fetch any repository (to build it) and report on
the builds it was given. It can't push, review, or act as a user. Build
commands run with the node machine's permissions, so treat nodes as part of
your CI's trust boundary.
