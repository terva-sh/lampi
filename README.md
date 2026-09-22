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
the ones `config.json` allowlists. With no allow rule it refuses the
project; the shape of that file is under [Off-box raw](#off-box-raw).
A second `sync` of the same files uploads nothing. `status` prints the
machine id and whether `/healthz` answered.

`agent` with no subcommand prints the same discovery, pushes the
allowlisted sessions once, then watches. Growth calls that same push.
A failed push is tried again after a short wait, without waiting for
the file to grow. SIGTERM drains the outbox and exits. The server URL,
the device token, and the allowlist are read when the process starts;
restart it to reload them. The one-shot command is still `sync`.

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
whose transcripts you would not copy onto that disk in the clear. Ruleset
v1 scans for common tokens before the upload and quarantines a hit. It
does not rewrite the file, and it is not a promise that every secret is
caught.

## Commands

| Command | What it does |
|---------|----------------|
| `terva-lampi serve` | Lake. `GET /healthz`, blob check/put, manifests. |
| `terva-lampi agent` | This machine. `discover`, `machine-id`, `config`, `status`, or watch and upload until SIGTERM. |
| `terva-lampi sync` | One shot: allowlist, ruleset v1, watermark, outbox, then PUT missing blobs and POST manifests. |
| `terva-lampi status` | Machine id, session count, lake health. |
| `terva-lampi login` | Write `~/.config/terva-lampi/token` (mode 0600). |

`terva-lampi --help` lists them. `terva-lampi <command> --help` prints flags.

The machine id is a ULID created once in `~/.config/terva-lampi/machine.json`
(`XDG_CONFIG_HOME` when that is set). It is not a fleet origin.

## Off-box raw

Raw bytes leave the machine only for a project `config.json` allowlists.
The default is to refuse every project. A rule matches the session's
cwd (a path prefix, on a boundary), its terva cwd hash, or its git
remote. Every field set on a rule has to match. `projects.deny` wins
over allow. An empty rule matches nothing.

The cwd is the one in the terva meta line, not the path of the JSONL
file. Git remotes are folded before comparison, so
`git@github.com:terva-sh/lampi.git` and
`https://github.com/terva-sh/lampi` are the same remote. When the
session cwd still has a `.git`, the manifest records the remote named
origin. Any other remote is ignored, so `git_remote` stays empty and a
remote allow rule does not match. Allow those projects by cwd or cwd hash.

```json
{
  "server": "http://127.0.0.1:8787",
  "projects": {
    "allow": [
      {"cwd_prefix": "/home/drew/src/foo"},
      {"git_remote": "git@github.com:terva-sh/lampi.git"},
      {"cwd_hash": "a1b2c3d4e5f60708"}
    ],
    "deny": [
      {"cwd_prefix": "/home/drew/src/foo/private"}
    ]
  },
  "redaction": {"upload_hits": false}
}
```

`terva-lampi agent config` prints how many allow and deny rules are
loaded. `sync` names each refused session and exits non-zero.

Before a request is sent, ruleset v1 scans the file. v1 is the
high-signal shapes: cloud keys, personal access tokens, and private-key
blocks. It does not flag JWTs or generic `password=` / `api_key=`
lines. Those show up in ordinary transcripts, and a hit would quarantine
the upload. The manifest
stamps `redaction.ruleset` as `v1` and `redaction.status` as `scanned`
when there are no hits. A hit is appended to `quarantine.jsonl` in the
state directory (mode 0600) and is not uploaded. The log names the rule.
It does not contain the matched text. `redaction.upload_hits` is the
only override that uploads a hit, and the manifest status is then
`override` with the hit count. Leave it false.

`sync` and `agent` share this path: allowlist, scan, watermark plan,
outbox, upload, manifest ACK, then watermark commit and outbox ACK. An
unchanged file uploads no new blob. The cursor does not move if the
manifest POST fails. `agent` runs until SIGTERM, then tries the path
once more so a push that was in flight can finish.

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

The backlog starts where the scaffold stopped: lake placement and
retention, the terva watch / outbox / redact pipeline, Layer B dedup,
a normalize-and-search proof, and the later harness phases.

## Status

This tree compiles and moves allowlisted terva JSONL end to end against
a local lake. In: filesystem CAS, SQLite catalog, device-token file,
discovery of `$TERVA_HOME/sessions`, an fsnotify/poll watcher, a durable
outbox, per-path watermarks, a project allowlist, and ruleset v1.
`sync` runs that pipeline and writes the watermark only after the
manifest ACK. Out, on purpose: hashed tokens, tail assembly on the lake,
the long-running upload loop inside `agent`, Claude/Codex/OpenCode/Cursor,
and a normalizer. See [docs/architecture.md](docs/architecture.md).

## License

MIT. See [LICENSE](LICENSE).
