---
schema: 3
id: TKT-01M3NPFNJA8EVC096M62KWMRDG
title: "Search index WAL keeps its peak size: 845 MiB on the dev lake"
type: bug
status: in-progress
status_reason: null
priority: high
due_on: null
labels:
  - area/search
assignees: []
milestone: null
parent: TKT-01M3K45MQBRESGG5HEZZSZ3FDQ
origin: null
dependencies: []
blocks_on: none
references: []
claim:
  actor: agent:claude-code/65ab7244
  branch: search/wal-limit
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-123283bc
  commit: 93998cb107dfcd01c021cf76b22b1a0081fefb09
  session: null
  claimed_at: 2026-09-29T04:24:11Z
  expires_at: null
archive: null
created_at: 2026-09-29T04:24:05Z
updated_at: 2026-09-29T04:25:23Z
created_by:
  id: agent:claude-code/65ab7244
  name: ""
updated_by:
  id: agent:claude-code/65ab7244
  name: ""
extensions: {}
---

## Description

On the dev workstation's lake (v0.3.0), 2026-09-29 04:15 UTC, after the index-version-4 rebuild finished (610 sessions indexed, 713,305 rows):

- `search.db` 2,219,290,624 bytes, freelist 0
- `search.db-wal` 886,038,992 bytes
- `search.db-shm` 1,736,704 bytes

The operations page reported the search index at 2.9 GiB, and 845 MiB of it was the WAL.

Cause: the index sets `journal_mode(WAL)` but neither `journal_size_limit` nor a `wal_checkpoint(TRUNCATE)`. A checkpoint copies frames back into the main file, but SQLite never shrinks the WAL file without one of those. The WAL keeps the size of its largest stretch between resets. On this lake that was the rebuild: one transaction per session, the largest holding 27 MiB of text, with FTS5 automerges inside each transaction.

Fix: at the end of a reclaim, run `PRAGMA wal_checkpoint(TRUNCATE)`. Also open the index with a `journal_size_limit`, so a WAL whose truncate was skipped because a reader was busy shrinks at the next reset.

## Acceptance criteria

- [x] The search.db WAL is truncated after a pass that reclaimed
- [x] search.db opens with a journal_size_limit

## Implementation plan

1. `reclaim` ends with `PRAGMA wal_checkpoint(TRUNCATE)`. It runs after every pass that wrote, and while a merge is pending. A reader still on the WAL makes the checkpoint report busy rather than fail.
2. `indexDSN` adds `journal_size_limit(64 MiB)`, so a WAL whose truncate found a reader shrinks at the next reset.
3. Test: a pass that reclaims leaves a zero-byte WAL, and the pragma is set on the index's connections.

Alternatives considered:
- `journal_size_limit` alone. The WAL only shrinks when the next write resets it, so an idle index keeps its WAL until then.
- Checkpoint after each session's transaction. The autocheckpoint already does a passive one, and a truncate after each session would wait on readers up to 600 times a pass.
- Smaller transactions during a rebuild. That would change indexSession's one-transaction-per-session guarantee, which lets queries see a session whole. The WAL still needs a truncate afterwards.

## Notes

**agent:claude-code/65ab7244** at 2026-09-29T04:25:23Z

Built on search/wal-limit. TestAReclaimTruncatesTheWAL fails with the checkpoint replaced by a no-op and passes with it. GOFLAGS=-mod=mod just ci green on origin/main 93998cb.
