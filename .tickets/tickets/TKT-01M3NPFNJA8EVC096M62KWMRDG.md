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
updated_at: 2026-09-29T04:24:11Z
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

- [ ] The search.db WAL is truncated after a pass that reclaimed
- [ ] search.db opens with a journal_size_limit
