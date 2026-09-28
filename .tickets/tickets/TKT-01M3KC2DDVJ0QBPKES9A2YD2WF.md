---
schema: 3
id: TKT-01M3KC2DDVJ0QBPKES9A2YD2WF
title: Search index grows by a third after re-normalizing every session
type: bug
status: in-progress
status_reason: null
priority: normal
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
  actor: agent:claude-code/e4a47e8c
  branch: tickets/after-chunk-fold
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-e4a47e8c
  commit: 0aa456e9fd9d1f24cccd77d7030ea5fc75436f5c
  session: null
  claimed_at: 2026-09-28T14:28:49Z
  expires_at: null
archive: null
created_at: 2026-09-28T06:43:37Z
updated_at: 2026-09-28T14:28:49Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/e4a47e8c
  name: ""
extensions: {}
---

## Description

The search index went from 485.6 MiB to 659 MiB after
`serve normalize --all` re-projected all 92 sessions on 2026-09-28,
although the sessions did not change.

Suspected cause: each session's rows are deleted and reinserted into
the trigram FTS5 table, and deleted rows are dropped only when FTS5
segments merge. The file does not shrink without an `optimize` and a
`VACUUM`.

To do:

- Measure the same pass on a copy of an index.
- Check the effect of `INSERT INTO fts(fts) VALUES('optimize')` followed
  by `VACUUM`.
- Decide where compaction belongs: after an index pass that rewrote
  many sessions, or as part of `serve compact`.

This is separate from the size of a fresh index, which TKT-01M3K45MX
covers.

## Implementation plan

Keep search.db near its live size by reclaiming after each pass, not
by a separate command.

- Open the index with `auto_vacuum(INCREMENTAL)` ahead of
  `journal_mode(WAL)` in the DSN. Setting it in the schema does nothing:
  the WAL pragma has already written page 1, and a file takes an
  auto-vacuum mode only before it has pages. Bump `indexVersion` to 2
  so the existing file is rebuilt with it.
- After a pass that indexed or removed a session, or whose last merge
  still changed the index, run `INSERT INTO fts(fts, rank)
  VALUES('merge', -2000)` then `PRAGMA incremental_vacuum`.
- Later, and separately: re-index an appended generation by patching
  rows rather than rewriting the session, which also removes the CPU
  cost of re-indexing a large active session at every sync.

## Notes

**agent:claude-code/e4a47e8c** at 2026-09-28T14:28:49Z

### Measurements

This session's transcript (41 MiB, about 16k events) indexed at eight
successive generations, each a few events longer:

| after each pass              | file at gen 8 | cost per pass |
|------------------------------|---------------|---------------|
| nothing (current code)       | 98.6 MiB      | -             |
| incremental vacuum only      | 77.9 MiB      | ~30 ms        |
| merge -2000 + incr. vacuum   | 30.5 MiB      | ~0.4 s        |
| optimize + incr. vacuum      | 22.6 MiB      | ~0.6 s        |

The live size is 22.5 MiB (optimize then VACUUM). Without reclaiming,
the file levels off near 4.3 times that, because deleted rows stay in
FTS5 segments until a crisis merge and freed pages are never returned.

### Alternatives considered

- `optimize` after each pass keeps the file at its live size, but it
  rewrites the whole full-text index, so its cost grows with the lake
  (hundreds of MiB per pass on the hosted lake). The bounded merge
  costs the same at any size.
- A bounded merge with a positive rank (`merge 500`) did nothing: it
  waits for usermerge segments at one level. The negative rank forces it.
- `VACUUM` from `serve compact` would reclaim once and the file would
  grow back between runs, and it needs serve stopped.
- Converting the v1 file in place (`auto_vacuum` then `VACUUM`) keeps it
  searchable during the upgrade but rewrites 1.4 GiB under the lock;
  rebuilding from the normalized files is simpler and the page reports
  coverage while it catches up.
