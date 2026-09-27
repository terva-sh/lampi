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
(the XDG state dir `terva-lampi/`, override with `--data`). It writes
one stderr line per request, except a 200 `/healthz`. In another
terminal:

```bash
curl -sS http://127.0.0.1:8787/healthz
./bin/terva-lampi agent discover
./bin/terva-lampi sync
./bin/terva-lampi status
```

`agent discover` lists the files each harness would upload.
[Harnesses](docs/harnesses.md) says where each one is read. `sync`
pushes only the projects `config.json` allows, and refuses every
project until you add an allow rule; see
[Allowlist and redaction](docs/allowlist-and-redaction.md). A second
`sync` of the same files uploads nothing. `status` prints the machine
id, each harness, the outbox, the last sync, and the lake's health.

`agent` with no subcommand pushes once, then watches and pushes as
transcripts grow. [The agent](docs/agent.md) covers the debounce,
retries, and signals. The one-shot command is still `sync`.

To add another machine, register it with a one-time code from the
lake. The lake needs a token file (one operator token is enough) and
the URL agents reach it at:

```bash
./bin/terva-lampi login --token-file ./tokens/operator.token
./bin/terva-lampi serve --token-file ./tokens &
./bin/terva-lampi serve identity set-url https://lake.example
./bin/terva-lampi serve register --name laptop > laptop.code
```

and on the laptop:

```bash
terva-lampi register --code-file laptop.code --install-service
```

[Registration and lakes](docs/registration-and-lakes.md) explains the
checks `register` makes, what it writes, and the manual token-file
fallback. On Windows, restart the agent after registering to add the
lake.

### Development on a machine that runs lampi

On a machine that already runs a lake or an agent, use the `dev`
recipes instead. With no flags, `serve` writes to the agent's state
directory and binds the live lake's port. `sync` reads the real device
token, machine id, server URL, and allowlist. The recipes put config
and state under `.dev/` in the checkout, put the lake in `.dev/lake`,
and bind `127.0.0.1:18787`. Set `LAMPI_DEV_ADDR` to change the address.

```bash
just dev-serve              # make dev-serve
just dev status             # make dev ARGS=status
just dev sync               # refuses every project until .dev/config allows one
just dev-clean              # make dev-clean
```

The dev allowlist is `.dev/config/terva-lampi/config.json` and starts
empty. `XDG_CONFIG_HOME` also moves the default Cursor IDE and Cursor
CLI roots, so set `harnesses` there to read them.

## Browser dashboard

The optional read-only dashboard shows counts, harnesses, normalization status,
sessions, provenance and conflicts. It also reads normalized transcripts, searches
them by literal text or event kind, links to single events, and copies a span of
events as text. Enable it with `serve --web-config PATH` and
an OIDC provider/group mapping. Device tokens remain mandatory for ingestion,
even when the server binds loopback behind a proxy. Browser sessions and device
tokens cannot authorize each other's routes. Without web config the UI is disabled.

See [serving the dashboard](docs/web-dashboard.md) for IdP registration, the
server config, TLS and session behavior. The dashboard uses embedded Go templates
and assets, so `make build` produces everything; there is no frontend build.
Bulk export and ingestion charts are planned in later releases.

## Commands

[Command reference](docs/cli.md) lists every command, the export
formats, and the order in which a flag, the environment, and
`config.json` resolve a setting. `terva-lampi --help` lists the
commands, and `terva-lampi <command> --help` prints the flags.

## Off-box raw

Raw bytes leave the machine only for a project `config.json` allows,
and ruleset v2 scans every file before it is sent. A hit is held in
quarantine on the machine. [Allowlist and redaction](docs/allowlist-and-redaction.md)
has the rule semantics and the scan. [Harnesses](docs/harnesses.md#cursor-sessions-with-an-empty-cwd)
explains why some Cursor sessions are always refused.

To add a machine to a lake, or to report to more than one lake, see
[Registration and lakes](docs/registration-and-lakes.md).

## Packaging examples

`deploy/` holds examples. Nothing there is installed by `make build`.
Phase 0 places the lake on a small VPS
([docs/policy.md](docs/policy.md)). Bring-up is
[docs/vps-bringup.md](docs/vps-bringup.md). The units set no server URL
or token file, so the agent falls back to `http://127.0.0.1:8787` and
a token file under `~/.config/terva-lampi/` and a local lake works. Set
`server` in `config.json`, or `LAMPI_SERVER`, to the VPS HTTPS URL on a
machine that should upload there. Do not put that hostname in this tree.

| Path | What it is |
|------|------------|
| [deploy/systemd/](deploy/systemd/) | User service for `terva-lampi agent`, system service for `terva-lampi serve`, and an env file for each |
| [deploy/launchd/](deploy/launchd/) | launchd agent with the same placeholders |
| [deploy/config.json.example](deploy/config.json.example) | Optional client `harnesses` map: one harness off, one absolute `root`. The agent debounce at its defaults. Loopback server URL |
| [deploy/install-lampi-alias.sh](deploy/install-lampi-alias.sh) | Optional `lampi` symlink. Refuses to replace an existing `lampi`, and warns when that file looks like neurobin's LAMP installer |
| [hooks/terva-post-tool-enqueue.sh](hooks/terva-post-tool-enqueue.sh) | Supported optional `post_tool_use` acceleration. Sends SIGUSR1 to a running `terva-lampi`. Not installed by `make build`. The watch still uploads if the hook never runs |

The order in which a flag, the environment, and `config.json` set the
server, the token file, and each harness root is in
[Where a setting comes from](docs/cli.md#where-a-setting-comes-from).
[deploy/README.md](deploy/README.md) is the operator note.

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

`go test ./...` includes `internal/accept`, the MVP gate. That package
is `TestMVPAcceptance`: one fixture terva session against a local lake.
Ingest records a session uid and blob sha256, a second sync uploads
nothing, an append uploads only the new tail and moves the head, a copy
of that file from a second machine is a CAS hit with one provenance row
per machine, and a sqlite query of `terva-lampi export` finds the
fixture prompt. See [docs/architecture.md](docs/architecture.md).

## Documentation

| Doc | What's in it |
|-----|----------------|
| [docs/README.md](docs/README.md) | The documentation index, grouped by task |
| [docs/harnesses.md](docs/harnesses.md) | Where each harness is read, and Cursor's empty-cwd cases |
| [docs/allowlist-and-redaction.md](docs/allowlist-and-redaction.md) | Project allow and deny rules, the ruleset v2 scan, quarantine |
| [docs/agent.md](docs/agent.md) | The upload path, the watch, retries, and signals |
| [docs/registration-and-lakes.md](docs/registration-and-lakes.md) | Registering a machine, many lakes, lake profiles |
| [docs/cli.md](docs/cli.md) | Every command, export formats, setting precedence |
| [docs/architecture.md](docs/architecture.md) | What the lake is, what this tree implements, what is a stub |
| [docs/web-dashboard.md](docs/web-dashboard.md) | OIDC dashboard configuration, deployment and validation |
| [docs/web-api.md](docs/web-api.md) | Viewer metadata API, filtering, pagination and status meanings |
| [docs/web-ui-plan.md](docs/web-ui-plan.md) | Dashboard design and future retrieval/analytics implementation tickets |
| [docs/policy.md](docs/policy.md) | Phase 0: VPS host, retention, encryption, allowlist, machines |
| [docs/vps-bringup.md](docs/vps-bringup.md) | Phase 0 operator checklist: disk, loopback serve, TLS, device token |
| [docs/protocol.md](docs/protocol.md) | Capture protocol 1: hello, blob check, put, manifest |
| [docs/pr-reviews.md](docs/pr-reviews.md) | Pull requests on Forgejo and GitHub, `terva-review`, keeping `main` equal |
| [deploy/README.md](deploy/README.md) | Example units, Shape A `harnesses`, the optional `lampi` alias, the optional `post_tool_use` hook |
| [.tickets/epics.md](.tickets/epics.md) | Open epics. Generated; `git ticket check --fix` rewrites it |

## Work tracking

Open work is a git-ticket store in `.tickets/`. A ticket is Markdown
with YAML frontmatter, committed next to the code it describes. Drafts
sit in `.tickets/draft/`. The working set (ready, in progress, blocked,
review) sits in `.tickets/tickets/`.

Install **git-ticket v0.23.0**. Release archives are on the GitHub
releases page. `install.sh` checks the archive against `checksums.txt`
and puts the binary in `~/.local/bin` (pass `--prefix` for another
writable directory).

```bash
curl -fsSL https://raw.githubusercontent.com/terva-sh/git-ticket/v0.23.0/install.sh | sh
```

With Go, the same tag is:

```bash
go install github.com/terva-sh/git-ticket/cmd/git-ticket@v0.23.0
```

Git runs a program named `git-ticket` as `git ticket`.

```bash
git ticket ready                 # startable, unblocked
git ticket list --status draft  # filed, not yet promoted
git ticket show TKT-…
git ticket check
```

Writes are recorded as `human:sothr` (Drew Short). That actor is the
default in `.tickets/config.yml`, so a write with no `--actor` uses it.
`AGENTS.md` is the short workflow an agent session reads. `git ticket
instructions` prints the long form.

Phase 0 placement, retention, and encryption are in
[docs/policy.md](docs/policy.md). Phase 5 training export is
`terva-lampi export --format sharegpt`. That view strips ruleset v2
matches from plaintext training fields. The CAS and `--format events`
are not rewritten. The Cursor IDE `state.vscdb` reader and the
Cursor CLI `store.db` reader are separate corpora.

## Status

This tree compiles and moves allowlisted terva JSONL end to end against
a local lake. In: filesystem CAS, SQLite catalog, device-token file,
discovery of `$TERVA_HOME/sessions` plus optional `raati/` and `tasks/`
sidecars, an fsnotify/poll watcher, a durable outbox, per-path
watermarks, a project allowlist, and ruleset v2.
`sync` runs that pipeline and writes the watermark only after the
manifest ACK. Device tokens are stored as hashes. A strict append is
assembled on the lake. A stored terva transcript is projected to
schema_version 1 events, and `terva-lampi export` writes those events
as JSONL. `--format sharegpt` writes an allowlisted trajectory of
the same sessions, with `raw_sha256` on each row and ruleset v2
matches stripped from the plaintext training fields. `internal/accept`
is the MVP gate for the events path, and CI runs
it with the rest of `go test ./...`. Cursor IDE `state.vscdb` and
Cursor CLI `store.db` are snapshotted into filtered JSON exports.
They do not share a harness, a session, or a watermark. See
[docs/architecture.md](docs/architecture.md).

## License

MIT. See [LICENSE](LICENSE).
