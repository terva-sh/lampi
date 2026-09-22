# lampi

`terva-lampi` is a session lake for AI agent transcripts. One static Go
binary holds the lake (`serve`) and the agent that runs on each machine
(`agent`, `sync`).

lampi is Finnish for a pond, a small lake. The bytes from the machines
you work on collect there: raw harness files, content-addressed, so the
same transcript uploaded twice occupies one blob.

The command is **`terva-lampi`**. The repository is `lampi`. Those are
different on purpose.

## Why the binary is terva-lampi

A bare `lampi` on `PATH` already means something else.
[neurobin/lampi](https://github.com/neurobin/lampi) is a LAMP installer
that copies itself to `/usr/local/bin/lampi` (`lampi -i`, `lampi -n`).
It is old and still documented in blog posts. Shipping that name as the
primary command would replace it, or be replaced by it.

`terva-lampi` is free of that collision and sits in the same family as
the terva harness. An install may still add a `lampi` symlink for people
who want the short name. Before it does, check what `lampi` already is.
If `command -v lampi` is a shell script, or the file mentions neurobin,
leave it alone. Invoking this program under the name `lampi` prints that
warning itself.

The word also belongs to other products (Lampi AI at lampi.ai, a kids'
app, Finnish companies). The metaphor stays. The command people type does
not depend on winning that search.

## What it is for

terva, and later other harnesses, already write transcripts on the
machine where the agent ran. Nothing in the org collects those files
across a laptop, a desktop, and a VPS, or notices that two of them are
the same bytes.

`terva-lampi serve` is that collection: an HTTP ingest, a filesystem
blob store keyed by sha256, and a SQLite catalog. `terva-lampi sync`
walks `$TERVA_HOME/sessions`, asks which digests the lake already has,
and uploads the rest. Agents dial the lake. The lake does not listen for
inbound connections from the laptop's point of view beyond the one
process you started.

Fleet, in terva, is a live control plane: members dial a hub so one
browser can drive sessions. The lake stores bytes after the fact. The
protocols stay separate.

`terva-ext-session-search` is local recall for one project on one
machine. The lake is the central copy.

## Quickstart

From a checkout, with Go 1.27:

```bash
make build          # bin/terva-lampi; `just build` is the same binary
./bin/terva-lampi serve
```

`serve` listens on `127.0.0.1:8787` and prints the data directory
(the XDG state dir `terva-lampi/`, override with `--data`). In another
terminal:

```bash
curl -sS http://127.0.0.1:8787/healthz
./bin/terva-lampi agent discover
./bin/terva-lampi sync
./bin/terva-lampi status
```

`agent discover` lists JSONL files under `$TERVA_HOME/sessions` (or
terva's platform default when that variable is unset). `sync` pushes
them. A second `sync` of the same files uploads nothing. `status` prints
the machine id and whether `/healthz` answered.

`agent` with no subcommand prints the same discovery and then waits.
Filesystem watch is not implemented; the wait is a stub. Use `sync` to
push.

`login` writes a device token and does not print it:

```bash
./bin/terva-lampi login
./bin/terva-lampi serve --token-file ~/.config/terva-lampi/token
```

Copy that file to the lake host. With `--token-file`, `/v1` routes
require `Authorization: Bearer`. Without a token file, `serve` accepts
unauthenticated requests only on a loopback address and refuses any other
`--addr`. `/healthz` stays open and returns no catalog data. The server
compares the token in plaintext. That is a stub: do not upload a project
whose transcripts you would not copy onto that disk in the clear. Nothing
here redacts secrets.

## Commands

| Command | What it does |
|---------|----------------|
| `terva-lampi serve` | Lake. `GET /healthz`, blob check/put, manifests. |
| `terva-lampi agent` | This machine. `discover`, `machine-id`, `config`, `status`, or wait. |
| `terva-lampi sync` | One shot: hash, skip digests the lake has, PUT the rest, POST manifests. |
| `terva-lampi status` | Machine id, session count, lake health. |
| `terva-lampi login` | Write `~/.config/terva-lampi/token` (mode 0600). |

`terva-lampi --help` lists them. `terva-lampi <command> --help` prints flags.

The machine id is a ULID created once in `~/.config/terva-lampi/machine.json`
(`XDG_CONFIG_HOME` when that is set). It is not a fleet origin.

## Build

```bash
make test           # go test ./...
make build          # bin/terva-lampi
just ci             # vet, gofmt, test, build — what GitHub CI runs
```

The module is `terva.sh/lampi`, the same vanity prefix as `terva.sh/terva`.
Install from a checkout until a release is tagged.

```bash
go build -o bin/terva-lampi ./cmd/terva-lampi
```

Sibling terva-sh repos use a justfile rather than golangci-lint. CI is
`go vet`, `gofmt -l`, `go test ./...`, and a build. The Makefile repeats
those targets for a machine without `just`.

## Documentation

| Doc | What's in it |
|-----|----------------|
| [docs/architecture.md](docs/architecture.md) | What the lake is, what this tree implements, what is a stub |
| [docs/protocol.md](docs/protocol.md) | Capture protocol 1: hello, blob check, put, manifest |

## Status

This is a scaffold that compiles and moves terva JSONL end to end against
a local lake. In: filesystem CAS, SQLite catalog, device-token file,
discovery of `$TERVA_HOME/sessions`. Out, on purpose: fsnotify, redaction,
hashed tokens, tail-only upload, Claude/Codex/OpenCode/Cursor, and a
normalizer. The interfaces are in the tree so those pieces have a place
to land. See [docs/architecture.md](docs/architecture.md).

## License

MIT. See [LICENSE](LICENSE).
