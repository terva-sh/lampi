---
schema: 3
id: TKT-01M3D57Q04BVBB5H9CV6JWH454
title: sqlitesnap misses a checkpoint within one mtime tick
type: bug
status: in-progress
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
claim:
  actor: agent:claude-code/cd41c9ac
  branch: t3code/repository-orientation-setup
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-cd41c9ac
  commit: e82a8f388a0c1c05a5a6ce6f63b839bdc4060d6a
  session: null
  claimed_at: 2026-09-25T21:30:00Z
  expires_at: null
archive: null
created_at: 2026-09-25T20:48:44Z
updated_at: 2026-09-25T21:30:00Z
created_by:
  id: agent:claude-code/opus
  name: ""
updated_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
extensions: {}
---

## Description

`TestTakeGivesUpOnADatabaseThatAlwaysMoves` in `internal/adapter/sqlitesnap` fails every run on brokkr (Linux 6.12, `CONFIG_HZ=250`, 12 cores, `TMPDIR` on tmpfs) at `705a2b7`, while GitHub CI passes the same commit. The failure is `err <nil>`: `Take` returned a snapshot for a database that changed between every copy.

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
