---
schema: 3
id: TKT-01M3V1AJGSZB7C2C52JCA2SF88
title: search.db WAL is never capped or truncated; it reached 1.5 GiB
type: bug
status: archived
status_reason: null
priority: high
due_on: null
labels:
  - area/search
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive:
  archived_at: 2026-10-01T06:24:42Z
  from_status: draft
  reason: "Duplicate of TKT-01M3NPFNJA (Search index WAL keeps its peak size); the fix continues in PR #137"
created_at: 2026-10-01T06:09:45Z
updated_at: 2026-10-01T06:24:42Z
created_by:
  id: agent:claude-code/27b21f4b
  name: ""
updated_by:
  id: agent:claude-code/27b21f4b
  name: ""
extensions: {}
---

## Description

search.db's write-ahead log is never capped or truncated, so it stays at its largest size. On the internal lake on 2026-10-01:

- The storage report showed `search.db-wal` at 739 MiB.
- A `PRAGMA wal_checkpoint(TRUNCATE)` in the first run of `storage-v0.5.1-BKPCeV5d/measure.sh` took it to 0.
- When the script ran again shortly after, the log was back at **1,597,274,592 bytes** (1.5 GiB), and the second checkpoint took it to 0 again.

### Cause

`internal/recall/index.go` opens search.db with `auto_vacuum(INCREMENTAL)`, `busy_timeout(5000)`, `journal_mode(WAL)` and `synchronous(NORMAL)`. It sets no `journal_size_limit` and never calls `wal_checkpoint(TRUNCATE)`.

SQLite's automatic checkpoint (every 1000 pages) moves the log's pages back into the database and starts writing the log from its start again, but it does not shrink the file. So the WAL file keeps the size of the largest stretch written between checkpoints that could reset it. That stretch can be large: a forced FTS5 merge (`mergePages` = 2000 pages, 8 MiB) after every pass that deleted rows, plus the rows themselves, and a reader holding a snapshot stops the checkpoint from resetting the log.

The growth back to 1.5 GiB also says how much the index writes: well over a gigabyte between the two runs. That volume is a separate question, and TKT-01M3K45MX should look at it.

### Fix

- Set `journal_size_limit` on search.db, for example 64 MiB. SQLite then truncates the WAL to that size whenever a checkpoint resets it.
- After a pass that wrote rows, run `PRAGMA wal_checkpoint(TRUNCATE)` once it is idle, with a busy result treated as "try next pass".
- Check `catalog.db` for the same gap. Its WAL was 4 MiB in the report, but it has the same exposure.

## Acceptance criteria

- [ ] search.db sets journal_size_limit, and the WAL is truncated after a pass that wrote rows
- [ ] A test shows the WAL file shrinking back under the limit after a large indexing pass
- [ ] catalog.db is checked for the same gap, and fixed or ruled out

## Notes

**agent:claude-code/27b21f4b** at 2026-10-01T06:24:28Z

Duplicate of TKT-01M3NPFNJA (Search index WAL keeps its peak size), filed 2026-09-28 with its fix in PR #137, which was never reviewed or merged. That ticket was only on the unmerged branch search/wal-limit, so a search of main missed it. The fix continues there, with this ticket's catalog.db criterion added. Archived.

**agent:claude-code/27b21f4b** at 2026-10-01T06:24:42Z

archived from draft: Duplicate of TKT-01M3NPFNJA (Search index WAL keeps its peak size); the fix continues in PR #137
