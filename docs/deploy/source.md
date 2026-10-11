# From source

Needs Go (see `go.mod`), Node.js 22+, and git.

```sh
git clone https://github.com/blaxstar/citric && cd citric
make            # builds the web UI, then bin/citric
make clients    # bin/dist/citric-<os>-<arch>: the binaries /dl serves
make test
```

The web UI is embedded in the binary, so build it (`make web`) before
`go build`. Run a server with `bin/citric serve`, then copy `bin/dist` into
`<data_dir>/dist` (or set `dist_dir`) so `/install.sh` can hand out clients.
