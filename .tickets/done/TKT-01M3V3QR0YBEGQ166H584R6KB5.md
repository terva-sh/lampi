---
schema: 3
id: TKT-01M3V3QR0YBEGQ166H584R6KB5
title: "Release v0.5.2: search index and catalog WAL cap; tag and notes"
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/ci
  - area/ops
  - area/docs
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-01T06:51:54Z
updated_at: 2026-10-01T07:08:14Z
created_by:
  id: agent:claude-code/27b21f4b
  name: ""
updated_by:
  id: agent:claude-code/27b21f4b
  name: ""
extensions: {}
---

## Description

The owner asked on 2026-10-01 to cut the next patch release after v0.5.1, carrying the search index WAL fix. That request is this ticket's promotion and confirms the cut. The version, v0.5.2, follows from "patch release".

### What it carries

Since v0.5.1 (4ef0c50), main gained one change: **#137, TKT-01M3NPFNJA** (Search index WAL keeps its peak size: 845 MiB on the dev lake).

- `search.db` opens with `journal_size_limit` set to 64 MiB. Each reclaim ends with `PRAGMA wal_checkpoint(TRUNCATE)`, and a reclaim whose checkpoint a reader kept busy stays pending, so the next pass tries again.
- `catalog.db` opens with the same 64 MiB `journal_size_limit`.

On the internal lake, `search.db-wal` had regrown to 1.5 GiB after v0.5.1 (measured 2026-10-01, TKT-01M3K45MX).

There is no diff in `internal/normalize`, `internal/protocol`, `deploy/` or the `Dockerfile`. `internal/catalog/catalog.go` changes only a per-connection pragma.

- The catalog stays at schema 21, and no migration is appended.
- Normalizer output, the search index format and the protocol are unchanged. Agents need no upgrade.
- **Rollback** is swapping the binary back. Neither pragma is stored in the database file.

## Acceptance criteria

- [x] A lake at v0.5.1 (schema 21) starts on a build of the final main with nothing to migrate, passes health and fsck, and indexes with its WAL truncated
- [x] The release is tagged on both forges and its archives and image name the tag
- [x] Release notes say there is no migration, rollback is a binary swap, agents need no upgrade, and describe the WAL fix

## Implementation plan

1. Merge #137 (TKT-01M3NPFNJA) and rehearse on the final main.
2. Tag at a commit on both mains, push to origin and github, check the archives and the image's `--version` on both forges, and prepend these notes to both release bodies (with `####` headings, as v0.4.0 to v0.5.1 did).
3. Deploy to the internal lake only when the owner asks, as a separate ticket. It is a binary swap with no migration, and the restart cuts the 1.5 GiB `search.db-wal` back.

#### Upgrading from v0.5.1

- **Nothing to migrate.** The catalog stays at schema 21, and blobs, the search index format and the protocol are unchanged. To roll back, install v0.5.1 again. Agents need no upgrade: only the lake changed.
- **A large `search.db-wal` is cut back at start.** The index's first pass, which runs as serve starts, ends with a truncating checkpoint, so the space comes back with the upgrade.

#### Fixed

- **The search index's WAL no longer keeps its peak size.** SQLite never shrinks a WAL file by itself, so `search.db-wal` kept the size of the largest stretch of writes between resets: 845 MiB after a rebuild on one lake, and 1.5 GiB on another. Each reclaim now ends with a truncating checkpoint. When a reader holds the WAL, the checkpoint is retried on the next pass. Both `search.db` and `catalog.db` also open with a 64 MiB `journal_size_limit`, so a WAL is cut back whenever SQLite resets it.

## Notes

**agent:claude-code/27b21f4b** at 2026-10-01T06:52:10Z

### Rehearsal, 2026-10-01

The build was cc2fcf6, the head #137 merged as 360e460 (main b856d38 plus the WAL commits). The scratch lake was the v0.5.1 rehearsal's (created by v0.4.0, upgraded through v0.5.0 to v0.5.1 builds), with a web config so serve runs the search index, and a device token.

- **Seeded with v0.5.1.** The v0.5.1 release binary served it and a scratch agent uploaded a second session. `search.db-wal` was 45,352 bytes while it ran. Counts: 2 sessions, 2 artifacts, 2 provenance rows.
- **Upgraded.** `serve migrate --check` with the new build reported `catalog schema 21, up to date`, and serve logged the same at start, with no migration.
- **Indexed.** A third session uploaded under the new build was indexed (3 sessions in `docs`), and `search.db-wal` was 0 bytes afterwards while serve still ran.
- **Checks.** `normalize --status`: ready=3, failed=0. The next sync uploaded nothing (unchanged 3). Integrity ok, counts 3/3/3, and `serve fsck` checked 3 entries, 0 bad.

`GOFLAGS=-mod=mod just ci` passed on the merged tree, and Forgejo CI and terva-review passed on cc2fcf6.

**agent:claude-code/27b21f4b** at 2026-10-01T07:08:14Z

### Published, 2026-10-01

- **Tag.** v0.5.2 is an annotated tag on 360e460, main on both forges after `just sync-github --yes`. It was pushed to origin and github.
- **Workflows.** The GitHub release workflow (run 36828026273) succeeded. On Forgejo, Build Image and Build and Publish Release succeeded.
- **Archives.** Each forge's five archives match its own `checksums.txt`. The two checksum files differ because each forge builds separately: GitHub used go1.27.0 and Forgejo go1.27.1. Both linux_amd64 binaries print `terva-lampi v0.5.2 (360e46007ca2)`.
- **Image.** With podman, `ghcr.io/terva-sh/lampi:0.5.2` and `:latest` both print `terva-lampi v0.5.2 (360e460)`.
- **Notes.** The plan's notes were prepended to both release bodies, with `####` headings.

Deploying to the internal lake waits for the owner to ask.

## Summary

v0.5.2 tagged at 360e460 and published on both forges with notes; archives and the GHCR image name v0.5.2. It carries #137 (TKT-01M3NPFNJA): search.db and catalog.db open with a 64 MiB journal_size_limit, and each index reclaim ends with a truncating checkpoint that is retried while a reader holds the WAL. No migration (schema 21), rollback is a binary swap, agents need no upgrade. Deploy is not part of this ticket.
