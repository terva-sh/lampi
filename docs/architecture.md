# Architecture

lampi is the session lake for the terva-sh org. One Go module, one binary,
`terva-lampi`, with the client and the server as subcommands. It is not
part of the terva harness repo: the release cadence and the set of
harnesses are different. It is also not fleet. Fleet is a live control
plane (members dial a hub so one browser can drive sessions). The lake
is push of immutable bytes into a content-addressed store plus a catalog.

The module path is `terva.sh/lampi`, the same vanity prefix as `terva.sh/terva`.

## What this tree does

| Piece | Package | State |
|-------|---------|--------|
| CLI dispatch | `internal/cli` | `serve`, `agent`, `sync`, `status`, `login`, `export`, `conflicts` |
| Wire types | `internal/protocol` | Capture protocol 1. See [protocol.md](protocol.md) |
| Blob store | `internal/cas` | Filesystem, key `sha256/<ab>/<rest>`, idempotent put |
| Catalog | `internal/catalog` | SQLite. Session uid, project id, artifacts, provenance |
| HTTP | `internal/api` | healthz, catalog stats, divergent_copy list, hello, blob check/put, manifests |
| Device token | `internal/auth` | 256-bit file, mode 0600. SHA-256 hash at rest |
| Machine id | `internal/config` | ULID in `~/.config/terva-lampi/machine.json` |
| terva discovery | `internal/discover`, `internal/adapter/terva` | `$TERVA_HOME/sessions/**/*.jsonl`, error sidecars, optional `raati/` records and `tasks/` archives |
| Claude Code | `internal/adapter/claude` | `$CLAUDE_CONFIG_DIR/projects/**/*.jsonl`. Unset is `~/.claude`. Reader version pinned. Unknown keys kept |
| Codex CLI | `internal/adapter/codex` | `$CODEX_HOME/sessions/**/rollout-*.jsonl`. Unset is `~/.codex`. `history.jsonl` is not a rollout |
| OpenCode | `internal/adapter/opencode` | Scheduled `opencode export` JSON at `$XDG_DATA_HOME/opencode/export/**/*.json`. Unset is `~/.local/share/opencode`. When `export/` has no JSON, the database file at that root. Not the WAL |
| Cursor IDE | `internal/adapter/cursor` | Read-only snapshot of `state.vscdb` under the user-data directory. Filtered JSON export. Keys under `cursorAuth/` are dropped. Not the CLI store |
| Cursor CLI | `internal/adapter/cursorcli` | Read-only snapshot of `store.db` under the CLI config directory. Separate harness `cursor-cli`. Not assumed to match IDE state |
| Watch | `internal/watch` | fsnotify, poll fallback, append offset. One layout per harness |
| Outbox | `internal/outbox` | SQLite queue of digests and manifest versions |
| Watermarks | `internal/watermark` | Per-path cursor, written only after a manifest ACK |
| Redaction | `internal/redact` | Ruleset v1. Upload hits are quarantined and the bytes are not rewritten. The training projection strips matches from its own copy |
| Allowlist | `internal/config` | cwd prefix, git remote, terva cwd hash. Default deny |
| Push | `internal/upload` | Allowlist, scan, watermark plan, outbox, put, manifest ACK, last-sync stamp. Files over the blob cap are chunked |
| Normalize | `internal/normalize` | Workers project terva, Claude Code, Codex CLI, OpenCode, Cursor IDE, and Cursor CLI onto schema_version 1 events. Unknown fields kept. `encrypted_content` stays opaque. Parquet is partitioned by UTC date and harness |
| Export | `terva-lampi export` | Normalized JSONL (`--format events`), or an allowlisted ShareGPT/trajectory JSONL. Training rows keep `raw_sha256`. `encrypted_content` stays opaque. Plaintext training fields are stripped with ruleset v1 |
| MVP gate | `internal/accept` | Five architecture §7 tests against a local lake |

`terva-lampi agent` lists those files, watches them, and uploads through
`upload.Sync`. `terva-lampi sync` is the same function, once. An unchanged
file uploads nothing. A strict append uploads only the new tail;
`byte_watermark_prev` is the previous length and `tail_sha256` is the
hash of those bytes. The lake assembles the tail onto the stored prefix
and moves the head. Bytes that are not a prefix either way are stored
as `divergent_copy` and the previous head stays. `terva-lampi conflicts` lists those rows, and `GET /v1/conflicts` returns the same list. A failed push is tried
again after a short wait. SIGTERM stops the watch and drains the outbox
best-effort. The server URL, token, and allowlist are read at start.

After a manifest is stored, the lake ACKs and enqueues normalize work.
The HTTP handler does not project. Two workers read the catalog head
and write the derived view. That view lags the ACK until the worker
for that generation finishes. `terva-lampi serve` drains the queue on
exit. A newer ingest bumps `sessions.normalize_gen` and a publish for
an older generation is dropped. The job row stays until the matching
generation is published, so a restart finishes it.

JSONL stays one file per session at `normalized/<session_uid>.jsonl`.
Parquet is hive-partitioned beside it. `github.com/parquet-go/parquet-go`
writes the files. It is pure Go, so the binary stays cgo-free.

```text
parquet/date=YYYY-MM-DD/harness=<harness>/<session_uid>.parquet
```

`date` is the UTC day of the event's `recorded_at`. When that timestamp
is missing, the day is `ingested_at`. A session whose events fall on
more than one day has one file in each of those partitions. `harness`
is the event harness (terva, Claude Code, Codex CLI, OpenCode, Cursor IDE,
or Cursor CLI). The file name is the session uid, so a re-projection
replaces that session and leaves the rest of the day in place. DuckDB
reads the tree with
`read_parquet('parquet/**/*.parquet', hive_partitioning = true)`.
Each row has the search columns (`content_text`, `session_id`,
`event_type`, `recorded_at`) and `event_json`, the same object as the
JSONL line.

A failure sets `sessions.normalize_error` and deletes that session's
JSONL and parquet files. The CAS object is not opened for write.
Unknown harness fields are kept on the event. `encrypted_content` is
copied through as an opaque string and is not written into
`content_text`. Image bytes stay in the raw blob. Pre-compaction rows
stay in the projection so an earlier prompt is still searchable.
`terva-lampi export` waits until the queue is idle. `--format events`
(the default) writes one JSON object per event. A missing JSONL file
is projected once, so a removed derived view can be rebuilt. DuckDB
reads that export with `read_ndjson`. sqlite reads each line and uses
`json_extract(line, '$.content_text')`. A session with `normalize_error`
set is skipped. Until a worker finishes, `normalize_error` is empty
and the derived files may be absent.

`--format sharegpt` and `--format trajectory` write the same training
projection: one ShareGPT conversation per allowlisted session that
has a training turn. A session with none is named on stderr. Turns
are message, tool call, tool result, compaction, and error rows.
`content_text` becomes `value`. A tool call keeps its name and call
id. Meta, usage, and unknown rows are left out. The `projects`
allowlist in `config.json` gates the file, the same default-deny
rules as off-box raw. The check uses the stored manifest cwd, cwd
hash, and git remote. A session that is not permitted is named on
stderr and omitted. Each row carries `raw_sha256`, the current
transcript blob, so the row can be traced without rewriting the CAS.
`encrypted_content` is copied onto the turn as stored. It is not
written into `value` and it is not decrypted. Ruleset v1 replaces
each match in the plaintext training fields (`value`, tool name, and
call id) with a placeholder that names the rule. The CAS object and
the normalized JSONL are not rewritten. `--format events` writes
those events unchanged.

## What this tree does not do

Left as interfaces, with the reason next to the type:

| Package | Later work |
|---------|------------|
| `internal/normalize` | A harness other than terva, Claude Code, Codex CLI, OpenCode, Cursor IDE, or Cursor CLI is stored, and the worker sets `normalize_error` |

`internal/watch`, `internal/outbox`, `internal/watermark`, and
`internal/redact` are implemented. `terva-lampi sync` and
`terva-lampi agent` both enqueue, scan, and advance a watermark after
the manifest ACK. The agent is the long-running loop: startup sync,
then a sync when the watcher reports growth or a previous push failed,
then one more sync on SIGTERM. On Unix, SIGUSR1 asks for a sync without
waiting for the next filesystem event. The agent writes `agent.pid` in
the state directory while it runs. Example units live under
`deploy/`. They are not installed by this tree. The agent unit is a
user service. The serve unit is a system service and binds loopback.
Phase 0 places the lake on a small VPS.
[policy.md](policy.md) is that decision: retention, encryption at
rest, the allowlist, and which machines run the agent.
[vps-bringup.md](vps-bringup.md) is the operator checklist. The
examples keep a loopback placeholder. On a machine that should
upload, set `LAMPI_SERVER` to the VPS HTTPS URL. Restart the agent
to reload config.

`hooks/terva-post-tool-enqueue.sh` is a supported optional acceleration
for terva `post_tool_use`. `make build` does not install it. On Unix
it sends SIGUSR1 to the pid in `agent.pid` when that process is the
`terva-lampi` executable. The signal asks for a sync now. The hook
can run before the session file is flushed, and it exits 0 when it
cannot signal. The directory watch is the source of truth while the
agent is running. A down agent uploads on its next start. Wiring is
in [deploy/README.md](../deploy/README.md).

Dedup that **is** implemented is layer A and layer B. Layer A is
`sha256` of the bytes: a second put of the same digest stores nothing.
Layer B is the logical session. `session_uid` is assigned once per
`(harness, native_session_id)`. An alias maps
`(harness, native_id, machine_id)` to that uid. The same digest from a
second machine adds a provenance row and no blob. A strict prefix
extension moves the head (`grown_from`). Anything else is
`divergent_copy` and is not merged.

A git-ticket claim does not resolve to that uid. The decision is to
leave the claim unwired. A claim stays an opaque string in the ticket
store. `session_uid` stays an identity this lake assigns at ingest.

An actor `agent:terva/session-…` is the name git-ticket's examples put
on a claim. Terva itself writes `agent:terva/` plus a persona, as in
`agent:terva/mieli`. The suffix is a label for who holds the ticket.
The catalog key for a terva transcript is `(terva, native_session_id)`.
`internal/adapter/terva` sets `native_session_id` from the `id` on the
first `type: meta` line, and from the filename stem when that line has
no id. The stem terva generates is `YYYYMMDD-HHMMSS-` plus eight hex
digits, which is the id `--resume` accepts. The meta `id` is a separate
UUID (`Session.ID`). Schema 3 `claim.session` is free text. git-ticket
stores it and does not parse it. When terva fills it, the value is that
meta UUID, taken from the session task board at claim time, and terva
leaves the field empty when the session has no task board. In the case
where the field holds the meta UUID and the transcript has been
ingested, the string is already `native_session_id`. `session_uid` is a
different value, a ULID from `internal/id.New` for that
`(harness, native_session_id)` pair. Normalized events name the session
`terva:` plus the native id. A claim carries no `machine_id`, so it
does not select an alias row. An alias is
`(harness, native_session_id, machine_id)`.

These strings do not identify a catalog row. The actor suffix, whether
`session-…` or a persona, is not the meta UUID and is not the filename
stem. The filename stem is not the meta UUID, so a claim or a resume id
made of the stem misses the row the adapter stored under the meta UUID.
An empty `claim.session` matches nothing. A hand-typed value matches
only by coincidence. git-ticket's own examples put a ULID in
`claim.session`. That alphabet is the one `session_uid` uses, and the
example value is not a uid this lake minted and not a terva meta UUID.

No package, command, or HTTP route in this tree reads a ticket.
`export`, `conflicts`, and normalize select a session by `session_uid`
or by `(harness, native_session_id)`. The bytes already carry the
native id. A resolver would parse a store this lake does not own, for a
question those paths do not ask, and it would treat free text as a
native id the claim format does not promise.

Layer C is the project. `project_id` is the normalized origin URL and
the repository root commit (`protocol.ProjectLinkID`). Two sessions
share it when those two inputs match, including when the checkouts
sit at different absolute paths and so have different `cwd_hash`
values. HEAD stays on `git_commit`. The link uses the root commit. A checkout with
no origin, no root commit, or a shallow history stores an empty
`project_id`. Empty ids are not a group. The lake recomputes the id
on ingest.

Near-duplicate detection is out of scope. A file over `max_blob_bytes`
is stored as CAS chunks of at most that size. The manifest lists the
digests and the lengths. The single-object cap still applies to a PUT,
a byte range, and one chunk. The logical file is not installed as one
object.

## Flow

```text
$TERVA_HOME/sessions/**/*.jsonl
$TERVA_HOME/raati/raati-*.json
$TERVA_HOME/tasks/tasks-*.json
$CLAUDE_CONFIG_DIR/projects/**/*.jsonl
$CODEX_HOME/sessions/**/rollout-*.jsonl
$XDG_DATA_HOME/opencode/export/**/*.json
Cursor IDE user-data/User/globalStorage/state.vscdb
Cursor IDE user-data/User/workspaceStorage/*/state.vscdb
Cursor CLI config/chats/*/*/store.db
        |
        v
allowlist (default deny) → ruleset v1 → quarantine on a hit
        |
        v
watermark plan → outbox → PUT missing blobs → manifest ACK
        |
        v
watermark commit and outbox ACK          terva-lampi serve
                                         CAS + SQLite catalog
                                         ACK, then normalize workers
                                         normalized/*.jsonl
                                         parquet/date=*/harness=*/*.parquet
                                         terva-lampi export → events JSONL
                                         or allowlisted ShareGPT JSONL
```

Other harnesses are adapters behind the same manifest. terva, Claude
Code, Codex CLI, OpenCode, and the Cursor IDE are wired for discovery,
watch, and upload. OpenCode watches `export/`, not the live database.
Cursor copies `state.vscdb` and its WAL sidecars, then uploads a JSON
export. Keys under `cursorAuth/` are not in that export. The raw
database stays on the machine. The global database has an empty cwd
and the allowlist refuses it by design. A workspace export copies
that global database read-only and merges `cursorDiskKV` rows for
composers named by the workspace ItemTable key
`composer.composerHeaders`. The session id stays `workspace/<id>`.
The global export itself still does not leave the machine. A
workspace database takes its cwd from `workspace.json`. A URI with no local path is an
empty cwd too. The Cursor CLI `store.db` is a second
harness, `cursor-cli`. It copies that database and its WAL sidecars
the same way and uploads a separate JSON export. It does not read
`state.vscdb`, and the IDE reader does not read `store.db`. The two
do not share sessions or watermarks. A CLI chat needs an absolute
`cwd` in the sibling `meta.json`. A missing file, a relative path, or
a file URI leaves the cwd empty, and the allowlist refuses the export.
The workspace hash is not a path. `sync` names those empty-cwd
refusals on stderr. The `projects` allow and deny rules are unchanged.
The Claude, Codex, OpenCode,
and Cursor record shapes are internal to those packages. Each pins a
reader version on `harness_version` and keeps keys it does not
interpret. Normalize workers project terva, Claude Code, Codex CLI,
OpenCode, Cursor IDE, and Cursor CLI onto schema_version 1. A stored
manifest for a harness that has no projector sets `normalize_error`.
Path-based
`cwd_hash` is copied from terva and buckets one absolute path. The
same git repo at two paths hashes differently. Those checkouts link
by `project_id`.

## MVP acceptance gate

`internal/accept` is the MVP acceptance gate.
`go test ./...` runs it, and that is what CI runs. The test is
`TestMVPAcceptance`. It stands up `terva-lampi serve`'s HTTP handler on
a local lake, writes one fixture terva JSONL, and pushes it with
`upload.Sync`.

1. Ingest records a `session_uid` and the blob sha256.
2. A second sync uploads no blob.
3. An appended line uploads the tail only, and the head sha256 updates.
4. The same file synced from a second machine is a CAS hit. Provenance
   for that digest has one row per machine.
5. `terva-lampi export` writes the normalized JSONL, and sqlite
   `json_extract(line, '$.content_text')` finds the fixture prompt
   `normalize-proof prompt: lampi-pond-7f3a`.

`internal/cli` still has the export failure-path test for a transcript
that does not normalize. That check is not a second copy of this gate.

## Layout

```text
cmd/terva-lampi/          the binary
internal/cli/             command dispatch
internal/protocol/        shared wire types
internal/api/             HTTP
internal/auth/            device token file
internal/cas/             filesystem blobs
internal/catalog/         SQLite
internal/config/          machine id and client config
internal/discover/        terva session walk, optional raati and tasks
internal/adapter/         harness interface
internal/adapter/terva/   meta line, manifests
internal/adapter/claude/  Claude Code projects/**/*.jsonl
internal/adapter/codex/   Codex rollout-*.jsonl, not history.jsonl
internal/adapter/opencode/ OpenCode export JSON, not the WAL
internal/adapter/cursor/  Cursor IDE state.vscdb snapshot
internal/adapter/cursorcli/ Cursor CLI store.db snapshot, separate corpus
internal/upload/          one-shot push
internal/watch/           fsnotify, poll fallback
internal/redact/          ruleset v1 and quarantine.jsonl
internal/outbox/          SQLite queue
internal/watermark/       per-path cursor, ACK-gated
internal/normalize/       schema_version 1 events, JSONL and parquet
internal/accept/          MVP acceptance gate, fixture terva JSONL
docs/protocol.md
docs/architecture.md
docs/policy.md            Phase 0 host, retention, encryption, inventory
docs/vps-bringup.md       Phase 0 operator checklist, not a provisioner
hooks/                    optional post_tool_use nudge, not installed
deploy/                   example agent and serve units, launchd, alias
```

`main` stays thin. The commands are an internal package because nothing
embeds them yet. git-ticket exports `cli` so terva can run `terva ticket`.
If terva ever grows a `terva lampi` alias, this package is what would move
out of `internal`.

## Auth and where the bytes sit

Single tenant, many devices, one token per device. `terva-lampi login`
writes a fresh 256-bit token and does not print it. The client reads
that file with `--token-file` and will not take the token as an argument.
Copy the file to the lake host. `terva-lampi serve --token-file` hashes
each line (or each file, when the path is a directory) and rewrites the
copy to `sha256:<hex>`. The client's file stays the secret. There is no
enrolment API.

Default bind is `127.0.0.1:8787`. A non-loopback `--addr` without
`--token-file` is an error. The data directory is the XDG state dir
`terva-lampi/` (override with `--data`), mode 0700, separate from
`$TERVA_HOME`. `$TERVA_HOME` is the producer. The lake does not write into it.

Secrets in transcripts are the main risk. Ruleset v1 runs before the
upload and refuses a hit unless `redaction.upload_hits` is set. The
allowlist is the other gate: a project that is not listed does not
leave the machine. Neither one rewrites the raw file. Do not point
`serve` at a network interface you do not control, and do not upload a
project you would not copy onto that disk in the clear.

Phase 0 runs that lake on a small VPS with local disk. Put TLS in
front of `serve`. The data disk uses the provider's volume encryption
and/or LUKS. There is no TTL and no application-level age wrapping.
Laptop, desktop, and a remote/cloud box run `terva-lampi agent`.
[policy.md](policy.md) is the record. [vps-bringup.md](vps-bringup.md)
is how an operator applies it.
