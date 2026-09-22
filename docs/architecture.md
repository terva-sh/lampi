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
| CLI dispatch | `internal/cli` | `serve`, `agent`, `sync`, `status`, `login` |
| Wire types | `internal/protocol` | Capture protocol 1. See [protocol.md](protocol.md) |
| Blob store | `internal/cas` | Filesystem, key `sha256/<ab>/<rest>`, idempotent put |
| Catalog | `internal/catalog` | SQLite. Session uid, artifacts, provenance |
| HTTP | `internal/api` | healthz, hello, blob check/put, manifests |
| Device token | `internal/auth` | 256-bit file, mode 0600. Plaintext compare |
| Machine id | `internal/config` | ULID in `~/.config/terva-lampi/machine.json` |
| terva discovery | `internal/discover`, `internal/adapter/terva` | `$TERVA_HOME/sessions/**/*.jsonl` and error sidecars |
| Watch | `internal/watch` | fsnotify, poll fallback, append offset |
| Outbox | `internal/outbox` | SQLite queue of digests and manifest versions |
| Watermarks | `internal/watermark` | Per-path cursor, written only after a manifest ACK |
| Redaction | `internal/redact` | Ruleset v1. Hits are quarantined, not rewritten |
| Allowlist | `internal/config` | cwd prefix, git remote, terva cwd hash. Default deny |
| Push | `internal/upload` | Allowlist, scan, watermark plan, outbox, put, manifest ACK |

`terva-lampi agent` lists those files, watches them, and uploads through
`upload.Sync`. `terva-lampi sync` is the same function, once. A grown
file is still one whole blob; the lake does not assemble a tail yet.
The manifest agrees: `byte_watermark_prev` is 0 and `tail_sha256` is the
full digest. An unchanged file uploads nothing. SIGTERM stops the watch
and drains the outbox best-effort.

## What this tree does not do

Left as interfaces, with the reason next to the type:

| Package | Later work |
|---------|------------|
| `internal/normalize` | Raw blob to the shared event schema |
| `internal/adapter` | Claude Code, Codex, OpenCode. Cursor is intentionally last and is not started |

`internal/watch`, `internal/outbox`, `internal/watermark`, and
`internal/redact` are implemented. `terva-lampi sync` and
`terva-lampi agent` both enqueue, scan, and advance a watermark after
the manifest ACK. The agent is the long-running loop: startup sync,
then a sync when the watcher reports growth, then one more sync on
SIGTERM. User-service packaging (systemd, launchd) is not in this tree.

`hooks/terva-post-tool-enqueue.sh` is an example nudge. A hook is not the
source of truth. The directory walk is.

Dedup that **is** implemented is layer A: `sha256` of the bytes. A second
put of the same digest stores nothing. The same native session id keeps
one `session_uid` and records each machine in provenance.

Dedup that is **not** implemented is the append-only prefix merge and
`divergent_copy`. A grown JSONL is uploaded whole under a new digest.
Near-duplicate detection is out of scope.

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
```

Other harnesses are adapters behind the same manifest. terva is the only
one wired up, because its JSONL layout is owned and versioned
(`format_version`). Path-based `cwd_hash` is copied from terva and is not
treated as a global project id. The same git repo at two absolute paths
hashes differently. Linking those by git remote is later work.

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
internal/normalize/       stub
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
writes the file and does not print the token. The lake process reads the
same kind of file with `--token-file`. There is no enrolment API: copy
the file. That is a stub, not a design to keep.

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
