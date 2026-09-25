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

`agent discover` lists files from four homes. terva is
`$TERVA_HOME/sessions` (or terva's platform default when that variable
is unset). Optional sidecars in that home are `raati/raati-<nanos>.json`
and `tasks/tasks-<session-id>.json`. They upload with a session the
allowlist already permits. A missing directory is skipped. Claude Code
is `$CLAUDE_CONFIG_DIR/projects/**/*.jsonl`, and
when `CLAUDE_CONFIG_DIR` is unset the directory is `~/.claude` (on
Windows, `%USERPROFILE%\.claude`). Codex is
`$CODEX_HOME/sessions/**/rollout-*.jsonl`, and when `CODEX_HOME` is
unset the directory is `~/.codex`. `history.jsonl` is Codex prompt
history and is not a rollout. OpenCode is a scheduled `opencode export`
at `$XDG_DATA_HOME/opencode/export/**/*.json`, and when `XDG_DATA_HOME`
is unset the directory is `~/.local/share/opencode` (on Windows,
`%USERPROFILE%\.local\share\opencode`). When `export/` has no JSON, the
database file at that root is listed instead. `opencode.db-wal` is not
read. An export uploads as `opencode_export_json`, and a re-export
replaces the session head. The record shape for Claude, Codex, and OpenCode is internal to
those adapters; each pins a reader version and keeps keys it does not
interpret. `sync` pushes the files `config.json`
allowlists. With no allow rule it refuses the project; the shape of
that file is under [Off-box raw](#off-box-raw).
A second `sync` of the same files uploads nothing. `status` prints the
machine id, one line per known harness
(`harness <id> enabled=<true|false> root=<absolute path or empty> source=<config|env|default>`),
outbox depth, watermark summary, last finished sync, whether
`/healthz` answered, and catalog counts from `GET /v1/stats`.
`source` is `config`, `env`, or `default`, naming which layer won.

`agent` with no subcommand prints the same discovery, pushes the
allowlisted sessions once, then watches. Growth calls that same push.
A failed push is tried again without waiting for the file to grow. The
first wait is 2s. Each further failure doubles the ceiling of a
jittered wait, up to 5 minutes, and a success resets it. Growth during
that wait does not start a push. A 401 or 403 is logged once, naming
the token file, and waits the full 5 minutes. A file that cannot be
read, or whose session id would sit on a line too long to read, is
skipped and named on stderr; the other files upload. A harness home
that does not exist yet, terva included, is polled until it appears.
A watcher that cannot use fsnotify, at the inotify watch limit or
after a queue overflow, polls that tree and says so. On macOS the
agent polls by default; `LAMPI_WATCH=fsnotify` or `LAMPI_WATCH=poll`
overrides that. A second agent on the same state directory exits and
names the first one's pid. SIGTERM drains the outbox and exits. The server URL,
the device token, the allowlist, and the harnesses map are read when
the process starts; restart it to reload them. The one-shot command
is still `sync`.

`login` writes a device token and does not print it:

```bash
./bin/terva-lampi login
./bin/terva-lampi serve --token-file ~/.config/terva-lampi/token
```

Copy that file to the lake host and pass the copy to `serve`. With
`--token-file`, `/v1` routes require `Authorization: Bearer`. `serve`
stores a SHA-256 of each device token and rewrites that copy; keep the
original as the client's secret. A token is 64 lowercase hex
characters, which is what `login` writes. A line starting with `#` is
a comment and stays through the rewrite. Any other line is an error.
In a token directory, only `<name>.token` files are loaded, one device
each. A file with another name, such as `laptop~` or
`laptop.revoked`, is named on stderr and not loaded. Earlier releases
loaded every file there, so rename device files to `.token` before
you upgrade. `kill -HUP` reloads the tokens without dropping requests
in flight. The token is not a command argument. Without a token file,
`serve` accepts unauthenticated requests only on a loopback address and
refuses any other `--addr`. With one, a non-loopback `--addr` is a
stderr warning: `serve` speaks plain HTTP, so TLS belongs in front.
Clients refuse to send a token to an `http://` URL unless the host is
`localhost`, 127.0.0.0/8, or `::1`. `/healthz` stays open and returns no catalog
data. Do not upload a project whose transcripts you would not copy onto
that disk in the clear. Ruleset v2 scans for common tokens before the
upload and quarantines a hit. It does not rewrite the file, and it is
not a promise that every secret is caught. It also scans the manifest,
which carries the cwd, the remote, and the relpaths. A hit there is
refused even with `redaction.upload_hits` set. A file that changed
after it was hashed is not sent in that sync; the next one sends it.

## Commands

| Command | What it does |
|---------|----------------|
| `terva-lampi serve` | Lake. `GET /healthz`, `GET /v1/stats`, `GET /v1/conflicts`, blob check/put, manifests. |
| `terva-lampi serve backup` | Copy the catalog (`VACUUM INTO`), the CAS, and the token file to `--out`. Runs while `serve` runs. |
| `terva-lampi serve fsck` | Re-hash every CAS object and name the bad ones. `--repair` removes them, with `serve` stopped. |
| `terva-lampi serve purge` | Remove one session: its catalog rows, derived files, and the blobs no other session names. Dry run without `--yes`. `serve` stopped. |
| `terva-lampi agent` | This machine. `discover`, `machine-id`, `config`, `status`, or watch and upload until SIGTERM. |
| `terva-lampi sync` | One shot: allowlist, ruleset v2, watermark, outbox, then PUT missing blobs and POST manifests. |
| `terva-lampi status` | Machine id, one line per harness (`enabled`, `root`, `source`), outbox, watermarks, last sync, lake health and catalog counts. |
| `terva-lampi login` | Write `~/.config/terva-lampi/token` (mode 0600). |
| `terva-lampi export` | Write normalized events as JSONL, or an allowlisted ShareGPT/trajectory dataset (`--format sharegpt`). |
| `terva-lampi conflicts` | List `divergent_copy` artifacts from the catalog: session, digests, and machines. |

`terva-lampi export --format events` (the default) writes one
normalized event per line. `--format sharegpt` and `--format
trajectory` write one ShareGPT conversation per session that
`config.json` allowlists and that has a training turn. A session
with no training turn, and a session that is not permitted, are
named on stderr and left out. Each training row carries
`raw_sha256`, the current transcript blob. `encrypted_content` is
copied onto the turn as stored and is not decrypted. Ruleset v2
strips matches from the plaintext training fields (`value`, tool
name, and call id). The command does not rewrite the CAS or the
normalized events, and `--format events` is not stripped. While
`serve` runs on the same `--data`, export reads the catalog
read-only and starts no normalize worker. A session that `serve` has
not normalized yet is named on stderr and left out.

`terva-lampi --help` lists them. `terva-lampi <command> --help` prints flags.

The machine id is a ULID created once in `~/.config/terva-lampi/machine.json`
(`XDG_CONFIG_HOME` when that is set). It is not a fleet origin.

## Off-box raw

Raw bytes leave the machine only for a project `config.json` allowlists.
Phase 0 confirms that default-deny surface in
[docs/policy.md](docs/policy.md).
The default is to refuse every project. A rule matches the session's
cwd (a path prefix, on a boundary), its terva cwd hash, or its git
remote. Every field set on a rule has to match. `projects.deny` wins
over allow. An empty rule matches nothing.

A deny rule reads a doubt as a match. Its `cwd_prefix` ignores case,
and it is checked against the cwd and the prefix as written and with
symlinks resolved. Its `cwd_hash` also matches the hash of the
resolved cwd. Its `git_remote` also matches a session whose remote
cannot be read: the cwd is gone, or the checkout has no readable
origin. A cwd that exists outside any repository has no remote, and a
`git_remote` deny does not match it. Add a `cwd_prefix` to a
`git_remote` deny to limit it to one tree. An allow rule compares
exactly, so a case or symlink variant of an allowed path is refused.

The cwd is the one in the terva meta line, not the path of the JSONL
file. Git remotes are folded before comparison, so
`git@github.com:terva-sh/lampi.git` and
`https://github.com/terva-sh/lampi` are the same remote. When the
session cwd still has a `.git`, the manifest records the remote named
origin. A URL remote loses its user part and password, except that an
ssh URL keeps a bare login name such as `git`. Any other remote is
ignored, so `git_remote` stays empty and a remote allow rule does not
match. Allow those projects by cwd or cwd hash.

The agent reads the remote, HEAD, and the root commit from files. It
runs `git` only for a session the allowlist admitted, and only when
the root commit is somewhere its reader does not follow. That call
pins config so the checkout's own settings cannot start a transport,
a hook, or another program.

The lake's `project_id` is not the cwd hash. It is that same folded
origin URL joined with the repository's root commit, so two machines
with the same remote and root share a project even when the absolute
paths differ. A shallow clone, or a checkout with no origin, has an
empty id and is not linked. See [docs/protocol.md](docs/protocol.md).

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

### Cursor sessions with an empty cwd

The `projects` allow and deny rules are the same for every harness.
An empty cwd matches no cwd prefix, no cwd hash, and no git remote,
so default deny keeps the export on the machine.

The Cursor IDE global database (`User/globalStorage/state.vscdb`) has
an empty cwd, so that export is refused by design. Current Cursor
builds keep chat bodies in the global database's `cursorDiskKV` table,
so a read of the workspace database alone misses type 1 and type 2
bubbles. The export copies the global database when
`composer.composerHeaders` names at least one composer. The snapshot
calls `copyTrio`, then opens the copy read-only. The workspace
database uses those same two steps. The reader does not open a live
database. The merge puts matching `cursorDiskKV` rows into the
workspace document field `cursor_disk_kv`. Membership is
`allComposers[].composerId` on the ItemTable key
`composer.composerHeaders`. `composer.composerData` is
the older workspace list and is not the registry. A composer listed
only on `composer.composerData` is not merged. A missing global file
adds nothing to the document. If the copy or the open fails, the
workspace export fails. The global export itself still does not leave
the machine. The Cursor IDE pinned reader `Version` is `2`, and the
document field `harness_version` is that string. `confidence` is
`low`. The native session id stays `workspace/<id>`. `terva-lampi
export --format events` writes `session_id` as `cursor:workspace/<id>`.
A workspace database takes its cwd from the folder URI in the sibling
`workspace.json`. A URI with no local path, such as `vscode-remote`,
is an empty cwd as well, and that workspace stays on the machine.

Normalize promotes a bubble tool out of that document. A
`toolFormerData` name and call id become a `tool_call`, and a result
string becomes a `tool_result`. A missing name or call id stays on
`extra`. On a `composerData:` row, `usageData` becomes a sibling
usage event only when it has a numeric `costInCents` or a recognizable
token count, and `latestConversationSummary` becomes a sibling
compaction event only when a summary string is present. Bubble
`usageData` and `tokenCount` stay on `extra`. The rules are in
[docs/architecture.md](docs/architecture.md#flow). The adapter
`Version` stays `2`. ShareGPT already includes that call.

A Cursor CLI chat takes its cwd from the `cwd` field of the sibling
`meta.json`, and only when that value is an absolute path. A missing
file, a relative path, or a file URI leaves the cwd empty, and the
allowlist refuses the export. The workspace directory name is a hash,
not a path.

When `sync` refuses a `cursor` or `cursor-cli` session whose cwd is
empty, the stderr line names which of those cases it is.

Before a request is sent, ruleset v2 scans the file. v2 is the
high-signal shapes: AWS keys, GitHub, GitLab, Slack, Anthropic, OpenAI,
Google, Stripe, npm, PyPI, Hugging Face, SendGrid, and DigitalOcean
tokens with their fixed prefixes, and PEM or PGP private-key blocks
from the BEGIN line through the END line. It does not flag JWTs or
generic `password=` / `api_key=` lines. Those show up in ordinary
transcripts, and a hit would quarantine the upload. The AWS
documentation example keys and Slack's placeholder webhook are not
hits. The scan reads JSON string escapes as the text they stand for,
so a key after an escaped line break, or a private key whose line
breaks are `\n` escapes, is still found. A Cursor value that the
export holds as base64 is scanned before it is encoded. The manifest
stamps `redaction.ruleset` as `v2` and `redaction.status` as `scanned`
when there are no hits. v1 missed keys inside JSON escapes. Manifests
it stamped stay on the lake as they are. A hit is appended to `quarantine.jsonl` in the
state directory (mode 0600) and is not uploaded. The log names the rule.
It does not contain the matched text. `redaction.upload_hits` is the
only override that uploads a hit, and the manifest status is then
`override` with the hit count. Leave it false.

`sync` and `agent` share this path: allowlist, scan, watermark plan,
outbox, upload, manifest ACK, then watermark commit and outbox ACK. An
unchanged file uploads no new blob. An append uploads the new tail
only; the lake assembles it onto the stored prefix. A file larger than
32 MiB is uploaded as chunks of at most that size. The lake keeps the
chunks and does not assemble one object past the cap. The cursor does
not move if the manifest POST fails. `agent` runs until SIGTERM, then
tries the path once more so a push that was in flight can finish. On
Unix, SIGUSR1 asks a running agent to sync. The process writes
`agent.pid` in the state directory while it runs. The directory watch
is still the source of truth.
[hooks/terva-post-tool-enqueue.sh](hooks/terva-post-tool-enqueue.sh)
is the optional terva `post_tool_use` acceleration that sends that
signal. `make build` does not install it.
[deploy/README.md](deploy/README.md) is how to wire it. If the hook
never runs, the watch still uploads, and a down agent uploads on its
next start.

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
| [deploy/config.json.example](deploy/config.json.example) | Optional client `harnesses` map: one harness off, one absolute `root`. Loopback server URL |
| [deploy/install-lampi-alias.sh](deploy/install-lampi-alias.sh) | Optional `lampi` symlink. Refuses to replace an existing `lampi`, and warns when that file looks like neurobin's LAMP installer |
| [hooks/terva-post-tool-enqueue.sh](hooks/terva-post-tool-enqueue.sh) | Supported optional `post_tool_use` acceleration. Sends SIGUSR1 to a running `terva-lampi`. Not installed by `make build`. The watch still uploads if the hook never runs |

`terva-lampi agent`, `sync`, `status`, and `agent config` read
`LAMPI_SERVER` and `LAMPI_TOKEN_FILE` when the matching flags are
unset. A flag wins, then the environment, then `config.json`, then the
default. `status` and `agent config` print `source=flag`, `env`,
`config`, or `default` on the `server` and `token_file` lines. The
`conflicts` token file follows the same order. Harness `root` is a different order: flag, if any, then
the `harnesses` entry in `config.json`, then the harness environment
variable, then the adapter default. That variable is a debug override.
Restart the agent after editing the map.
[deploy/README.md](deploy/README.md) is the operator note, including
the `status` line that names which layer won.

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
| [docs/architecture.md](docs/architecture.md) | What the lake is, what this tree implements, what is a stub |
| [docs/policy.md](docs/policy.md) | Phase 0: VPS host, retention, encryption, allowlist, machines |
| [docs/vps-bringup.md](docs/vps-bringup.md) | Phase 0 operator checklist: disk, loopback serve, TLS, device token |
| [docs/protocol.md](docs/protocol.md) | Capture protocol 1: hello, blob check, put, manifest |
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
