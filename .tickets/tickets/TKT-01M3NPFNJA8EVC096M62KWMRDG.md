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
updated_at: 2026-10-01T06:41:25Z
created_by:
  id: agent:claude-code/65ab7244
  name: ""
updated_by:
  id: agent:claude-code/27b21f4b
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
- [x] catalog.db opens with a journal_size_limit too

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

**agent:claude-code/27b21f4b** at 2026-10-01T06:24:57Z

### On the internal lake, 2026-10-01: picked up again

PR #137 passed CI on 2026-09-28 but was never reviewed or merged. The same failure reached the internal lake running v0.5.1:
- The storage report showed `search.db-wal` at 739 MiB.
- `measure.sh` ran `wal_checkpoint(TRUNCATE)` on the live index and took it to 0.
- When the script ran again shortly after, the WAL was back at **1,597,274,592 bytes**, and the checkpoint took it to 0 again.

TKT-01M3V1AJGS was filed for this without knowing about this ticket, and it is archived as a duplicate.

Picking it up again:
- **Updated:** origin/main is merged into search/wal-limit, which merged cleanly. `TestAReclaimTruncatesTheWAL` still passes.
- **Added:** `journal_size_limit(64 MiB)` on catalog.db's connections too, which was criterion 3 of the duplicate. The catalog's WAL was only 4 MiB, but it keeps its peak the same way. `TestPragmasSurviveNewConnection` now checks the limit on a fresh connection. Removing the pragma fails it with `journal_size_limit = -1, want 67108864`, and the driver's default is -1, so the test checks the setting and not a default.

**agent:claude-code/27b21f4b** at 2026-10-01T06:41:25Z

Review 1670 finding 1: reclaim discarded the wal_checkpoint(TRUNCATE) result row, so a reader on the WAL (busy=1) let a pass clear the pending reclaim with the WAL untruncated. Fixed in 1c37375: reclaim scans (busy, log, checkpointed) and returns more while busy. TestAReaderOnTheWALLeavesTheReclaimPending opens a deferred reader from beforeReclaim (indexDSN's _txlock=immediate would take the write lock instead) and fails without the fix.
