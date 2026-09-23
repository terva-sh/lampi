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
| CLI dispatch | `internal/cli` | `serve`, `agent`, `sync`, `status`, `login`, `export` |
| Wire types | `internal/protocol` | Capture protocol 1. See [protocol.md](protocol.md) |
| Blob store | `internal/cas` | Filesystem, key `sha256/<ab>/<rest>`, idempotent put |
| Catalog | `internal/catalog` | SQLite. Session uid, artifacts, provenance |
| HTTP | `internal/api` | healthz, catalog stats, hello, blob check/put, manifests |
| Device token | `internal/auth` | 256-bit file, mode 0600. SHA-256 hash at rest |
| Machine id | `internal/config` | ULID in `~/.config/terva-lampi/machine.json` |
| terva discovery | `internal/discover`, `internal/adapter/terva` | `$TERVA_HOME/sessions/**/*.jsonl` and error sidecars |
| Watch | `internal/watch` | fsnotify, poll fallback, append offset |
| Outbox | `internal/outbox` | SQLite queue of digests and manifest versions |
| Watermarks | `internal/watermark` | Per-path cursor, written only after a manifest ACK |
| Redaction | `internal/redact` | Ruleset v1. Hits are quarantined, not rewritten |
| Allowlist | `internal/config` | cwd prefix, git remote, terva cwd hash. Default deny |
| Push | `internal/upload` | Allowlist, scan, watermark plan, outbox, put, manifest ACK, last-sync stamp |
| Normalize | `internal/normalize` | terva JSONL to schema_version 1 events. Unknown fields kept. `encrypted_content` stays opaque |
| Export | `terva-lampi export` | Normalized JSONL. A session with `normalize_error` is skipped |
| MVP gate | `internal/accept` | Five architecture §7 tests against a local lake |

`terva-lampi agent` lists those files, watches them, and uploads through
`upload.Sync`. `terva-lampi sync` is the same function, once. An unchanged
file uploads nothing. A strict append uploads only the new tail;
`byte_watermark_prev` is the previous length and `tail_sha256` is the
hash of those bytes. The lake assembles the tail onto the stored prefix
and moves the head. Bytes that are not a prefix either way are stored
as `divergent_copy` and the previous head stays. A failed push is tried
again after a short wait. SIGTERM stops the watch and drains the outbox
best-effort. The server URL, token, and allowlist are read at start.

After a manifest is stored, the lake projects each terva transcript into
`normalized/<session_uid>.jsonl`. The job is synchronous. A failure sets
`sessions.normalize_error` and deletes that session's derived file. The
CAS object is not opened for write. Unknown harness fields are kept on
the event. `encrypted_content` is copied through as an opaque string and
is not written into `content_text`. Image bytes stay in the raw blob.
Pre-compaction rows stay in the projection so an earlier prompt is still
searchable. `terva-lampi export` writes one JSON object per event.
DuckDB reads it with `read_ndjson`. sqlite reads each line and uses
`json_extract(line, '$.content_text')`. A session with `normalize_error`
set is skipped. Async workers and parquet partitions are later work.

## What this tree does not do

Left as interfaces, with the reason next to the type:

| Package | Later work |
|---------|------------|
| `internal/adapter` | Claude Code, Codex, OpenCode. Cursor is intentionally last and is not started |

`internal/watch`, `internal/outbox`, `internal/watermark`, and
`internal/redact` are implemented. `terva-lampi sync` and
`terva-lampi agent` both enqueue, scan, and advance a watermark after
the manifest ACK. The agent is the long-running loop: startup sync,
then a sync when the watcher reports growth or a previous push failed,
then one more sync on SIGTERM. User-service packaging (systemd, launchd)
is not in this tree. Restart the agent to reload config.

`hooks/terva-post-tool-enqueue.sh` is an example nudge. A hook is not the
source of truth. The directory walk is.

Dedup that **is** implemented is layer A and layer B. Layer A is
`sha256` of the bytes: a second put of the same digest stores nothing.
Layer B is the logical session. `session_uid` is assigned once per
`(harness, native_session_id)`. An alias maps
`(harness, native_id, machine_id)` to that uid. The same digest from a
second machine adds a provenance row and no blob. A strict prefix
extension moves the head (`grown_from`). Anything else is
`divergent_copy` and is not merged.

Near-duplicate detection is out of scope. Chunked upload is not
implemented.

## Flow

```text
$TERVA_HOME/sessions/**/*.jsonl
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
                                         normalize → normalized/*.jsonl
                                         terva-lampi export → JSONL
```

Other harnesses are adapters behind the same manifest. terva is the only
one wired up, because its JSONL layout is owned and versioned
(`format_version`). Path-based `cwd_hash` is copied from terva and is not
treated as a global project id. The same git repo at two absolute paths
hashes differently. Linking those by git remote is later work.

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
internal/discover/        terva session walk
internal/adapter/         harness interface
internal/adapter/terva/   meta line, manifests
internal/upload/          one-shot push
internal/watch/           fsnotify, poll fallback
internal/redact/          ruleset v1 and quarantine.jsonl
internal/outbox/          SQLite queue
internal/watermark/       per-path cursor, ACK-gated
internal/normalize/       schema_version 1 events, JSONL export
internal/accept/          MVP acceptance gate, fixture terva JSONL
docs/protocol.md
docs/architecture.md
hooks/                    example terva hook, not installed
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
