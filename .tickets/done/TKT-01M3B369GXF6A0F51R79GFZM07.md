---
schema: 3
id: TKT-01M3B369GXF6A0F51R79GFZM07
title: "Cursor IDE capture: one global snapshot, SQL filter, safe copy"
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/adapter
assignees: []
milestone: null
parent: TKT-01M3B35JS8J2F83FG4J7ZC0199
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-25T01:34:31Z
updated_at: 2026-09-25T14:55:08Z
created_by:
  id: agent:claude-code/eh1m
  name: Claude Code cloud agent
updated_by:
  id: agent:claude-code/eh1m
  name: Claude Code cloud agent
extensions: {}
---

## Description

The Cursor IDE capture copies the global database once per workspace on every sync, and the snapshot can be torn.

### Findings

- Cost. Read. Each workspace export copies the whole global `state.vscdb` trio to `$TMPDIR` (`mergeWorkspaceComposers` and `snapshotDisk`, `internal/adapter/cursor/snapshot.go:118-215`, `copyTrio` at `:298`), reads all of `cursorDiskKV` into memory before filtering (`snapshot.go:89,215`), and exports the global database in full, although the allowlist always refuses it (`internal/adapter/cursor/cursor.go:235-260`). `state.vscdb-wal` writes trigger syncs.
- Torn snapshot. Read, not reproduced. `copyTrio` (`cursor/snapshot.go:298`, `cursorcli/snapshot.go:355`) copies the database, `-wal`, and `-shm` one after another with no lock. A checkpoint between copies can pair an old main file with a new WAL.

### Approach

Snapshot the global database once per sync. Filter in SQL by composer id. Skip the global export, and any workspace the allowlist refuses by cwd, before exporting. Take snapshots with `VACUUM INTO` or the backup API on a read-only open, or re-stat after the copy, retry on change, and run `quick_check`.

## Acceptance criteria

- [x] One sync copies the global database at most once
- [x] Refused workspaces and the global database are not exported
- [x] A snapshot is taken consistently or retried

## Implementation plan

### Verified against main

- `exportDocument` copies the workspace trio, then `mergeWorkspaceComposers` calls `snapshotDisk`, which copies the global trio again and reads all of `cursorDiskKV` before `filterDisk`. With N workspaces that name composers, one sync copies the global database N times, plus once more for the global export itself.
- `Manifests` exports every database, including the global one and workspaces whose cwd the allowlist refuses. `internal/upload/prepare.go` refuses them only after the export was written and hashed.
- `copyTrio` in both packages copies db, -wal, -shm with no check. A checkpoint that resets the WAL between the main-file copy and the WAL copy pairs an old main file with new frames.
- `encodeValue` in both packages, and `presentMeta` in cursorcli, return a zero scan when `Ruleset.Scan` errors. That fails open for the value.

### Approach

1. New package `internal/adapter/sqlitesnap` holds the one snapshot routine both readers use. It copies the main file and the -wal (not the -shm, which the first connection to the copy rebuilds from the WAL anyway), then re-stats the main file and re-reads the WAL header (salts and checkpoint sequence). A main file that changed, or a WAL that was reset or removed, retries the copy, up to a bound. The copy is then opened read-only and `PRAGMA quick_check` must say ok, or that attempt is retried too. WAL appends during the copy are not a change: SQLite stops at the last valid commit frame.
2. Opening the live database read-only and running `VACUUM INTO` was tried and rejected: with no other connection open, a `mode=ro` open creates `-wal` and `-shm` beside the live file and rewrites `-shm` on every open. The watcher matches those sidecars, so every sync would trigger the next one. It would also break the documented rule that the reader does not open a live database.
3. The Cursor IDE reader shares one lazily taken global snapshot across a `Manifests` call. The global export reuses it when that session is exported. Workspace merges query `cursorDiskKV` per composer id in SQL: `composerData:<id>` by equality and `bubbleId:`, `checkpointId:`, `messageRequestContext:`, `codeBlockDiff:` `<id>:` by key range. `filterDisk` stays as the guard.
4. `adapter.Permit` is an optional predicate. `cursor.ManifestsPermit` and `cursorcli.ManifestsPermit` ask it with a manifest that carries the project and the artifact relpath before any export work. A refused session keeps that manifest, with no digest and no path, so `sync` still names it with the same refusal line. `internal/upload` passes `opt.Projects.Permitted(projectID(m))`, the same check `prepare` makes.
5. A scan error fails closed: `encodeValue` and `presentMeta` return it, and the export fails with an error naming the key. The scanner is a package variable so a test can make it fail.
6. Tests: global snapshot count per `Manifests`, SQL filter output, refused workspace not exported, retry after a WAL reset between copies and under a concurrent writer that checkpoints, scan error fails closed. Docs updated where they name `copyTrio`. Reader versions stay: the exported documents do not change.

## Notes

**agent:claude-code/eh1m** at 2026-09-25T14:54:46Z

Snapshot method: `VACUUM INTO` on a read-only open of the live database was tried and rejected. With no other connection open (Cursor closed), a `mode=ro` open created `state.vscdb-wal` and `state.vscdb-shm` beside the live file, and every later open rewrote `-shm` (new mtime). `Adapter.Match` treats both sidecars as changes, so each sync would trigger the next one. It would also break the documented rule that the reader never opens a live database. The copy path in `internal/adapter/sqlitesnap` is used instead: main file and WAL (the `-shm` is not copied; the first connection to the copy rebuilds it), then a re-stat of the main file (size, mtime, identity) and a re-read of the 32-byte WAL header (salts and checkpoint sequence), retried up to 5 times, then `PRAGMA quick_check(1)`. WAL appends during the copy do not force a retry, since SQLite stops at the last verifiable commit frame. A test that disables both checks fails the checkpoint-between-copies test; with either check alone it passes.

Refused sessions: `adapter.Permit` is an optional predicate. `cursor.ManifestsPermit` and `cursorcli.ManifestsPermit` ask it before any export work, with a manifest that carries the project and relpath but no digest. A refused session keeps that manifest, with no digest and no `Paths` entry, so `prepare` still refuses it with the same `allowlistRefusal` line and `dropPending` still acks its outbox identity. `internal/upload/upload.go` `bundlesFor` passes `opt.Projects.Permitted(projectID(m))`, the check `prepare` makes. That is a three-line edit in `internal/upload`.

Scan errors: `encodeValue` in both packages, and `presentMeta` / `presentBlob` in cursorcli, now return the scan error. The export then fails with an error naming the key or blob id, and Manifests fails as it already did for a failed copy. `Ruleset.Scan` cannot currently return an error, so this only guards the interface.

The reader `Version` values stay 2 (IDE) and 1 (CLI). The exported documents are byte-for-byte the same shape and hold the same rows.

## Summary

Landed on branch `claude/elegant-feynman-eh1mdd-cursor`.

- New `internal/adapter/sqlitesnap`: copies the main file and the WAL, re-stats the main file and re-reads the WAL header, retries up to 5 times on a checkpoint, and runs `quick_check`. Both Cursor readers use it. The live database is still never opened.
- Cursor IDE: one global snapshot per `Manifests` call, shared by the global export and every workspace merge. Composer rows are selected in SQL by `composerData:<id>` and by `<prefix><id>:` key ranges.
- `adapter.Permit`, `cursor.ManifestsPermit`, `cursorcli.ManifestsPermit`: sync passes the allowlist, so a refused session, including the global database, is not snapshotted or exported. Its digest-less manifest still gets the same refusal line.
- Scan errors in `encodeValue` and `presentMeta` fail the export and name the key.
- Tests: global copy count, SQL row selection, refused sessions skipped (adapter and `bundlesFor`), a retry after a checkpoint between copies, giving up on a database that always moves, a concurrent writer, and a scan error that fails closed. Docs updated. The reader versions stay 2 and 1.
