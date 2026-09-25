---
schema: 3
id: TKT-01M3D57Q04BVBB5H9CV6JWH454
title: sqlitesnap misses a checkpoint within one mtime tick
type: bug
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area/adapter
  - phase/4-cursor
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-25T20:48:44Z
updated_at: 2026-09-25T21:33:41Z
created_by:
  id: agent:claude-code/opus
  name: ""
updated_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
extensions: {}
---

## Description

`TestTakeGivesUpOnADatabaseThatAlwaysMoves` in `internal/adapter/sqlitesnap` fails every run on the owner's workstation (Linux 6.12, `CONFIG_HZ=250`, 12 cores, `TMPDIR` on tmpfs) at `705a2b7`, while GitHub CI passes the same commit. The failure is `err <nil>`: `Take` returned a snapshot for a database that changed between every copy.

### Cause (proven with a probe test, then removed)

`copyOnce` decides that a copy is stable from three signals: main-file size, main-file mtime, and the first 32 bytes of the WAL. After `PRAGMA wal_checkpoint(TRUNCATE)`, the WAL is zero bytes both before and after the copy, so its header compares equal. The main-file mtime did not change across a commit plus checkpoint in any of five attempts, because the kernel file timestamp comes from the coarse clock and one tick covers the whole write. When the checkpoint did not add pages, the size also matched. Probe output, one line per attempt:

```text
size 4096->12288  mtime_same=true wal=0   stable=false
size 12288->28672 mtime_same=true wal=0   stable=false
size 28672->28672 mtime_same=true wal=0   stable=true   <- missed
size 28672->36864 mtime_same=true wal=0   stable=false
size 36864->36864 mtime_same=true wal=0   stable=true   <- missed
```

### Why it matters beyond the test

In production this is the Cursor IDE `state.vscdb` snapshot. A checkpoint that rewrites main-file pages while `copyFile` reads can produce a torn copy, which none of the three signals catches. `openChecked` may catch some torn copies through an integrity check, but not a copy that is consistent and stale. mtime on Linux is not a change counter.

### Directions to weigh

- Compare a content signal of the main file rather than mtime, such as a hash of the copied bytes against a re-read after the WAL copy. The SQLite file change counter at header offset 24 does not help: in WAL mode SQLite does not promise to bump it on every transaction.
- Hold a read transaction on the source through the copy, which blocks a checkpoint from overwriting pages still in use, if opening the live database read-only is acceptable for Cursor.

## Implementation plan

Replace mtime as the main-file change signal with content. `copyFile` hashes the main file with SHA-256 while it copies. After the WAL copy and before the closing stat, `copyOnce` hashes the live main file again. A digest that differs marks the attempt unstable. The size, mtime, identity and WAL header checks stay; they are cheap and still catch a replaced file or a reset WAL.

### Why this direction

- A second read of the main file costs one more pass over the file per snapshot. The agent only snapshots after the watcher sees a change, so the mtime is nearly always recent. A fast path that skips the re-read when the mtime is older than a coarse tick would almost never apply, and it would add a clock heuristic that depends on the filesystem.
- A read transaction held on the live database was rejected. The package opens nothing live because a read-only open creates `-wal` and `-shm` beside the file and rewrites `-shm`. The watcher matches those files, so every sync would start another one.
- The SQLite file change counter at header offset 24 was rejected. In WAL mode SQLite does not promise to bump it for each transaction.

### What the digest catches

The copy passes only if it is byte-equal to the live main file as read after the WAL copy. A checkpoint between the two copies rewrites main pages, so the digests differ. A write during the main copy that touched a page already copied also leaves the copy different from the re-read. The one case it can miss is a page changed and then changed back between the two reads, which SQLite page writes do not do in practice.

A write between the two reads could shorten the file and then grow it back. That is caught when any page content differs.

## Summary

`copyOnce` in `internal/adapter/sqlitesnap` now compares content instead of trusting mtime. The main file is hashed as it is copied, and the live file is hashed again after the WAL copy. A mismatch marks the attempt unstable, and `Take` retries. The size, mtime, identity and WAL-header checks are kept.

Verified on the owner's workstation, where the test failed on every run before the change:

- `TestTakeGivesUpOnADatabaseThatAlwaysMoves` passes 20 out of 20 runs.
- New test `TestTakeRetriesWhenMainChangesWithinOneTick` rewrites a main-file page between the copies and restores the size and mtime. This reproduces the bug on any host, including CI, which the old test could not. It fails on the old code with "copy ... was stable" and passes with the fix.
- `just ci` and `go test -race ./...` are green.

Cost: one extra read of the main file for each snapshot. `TestTakeIsConsistentUnderAWriter` now runs to its 2s cap, because more attempts under its non-stop writer are correctly rejected as changed. It still gets consistent snapshots.
