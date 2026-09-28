---
schema: 3
id: TKT-01M3KC2DDVJ0QBPKES9A2YD2WF
title: Search index grows by a third after re-normalizing every session
type: bug
status: done
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
claim: null
archive: null
created_at: 2026-09-28T06:43:37Z
updated_at: 2026-09-28T16:14:27Z
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

Keep search.db near its live size, and index a session that grew by
writing only what changed.

Done in #63 and #72:
- Open the index with `auto_vacuum(INCREMENTAL)` ahead of
  `journal_mode(WAL)` (index version 2).
- After a pass that wrote rows, run a bounded merge and
  `PRAGMA incremental_vacuum`; keep it pending across failures and
  stopped passes.

This part:
- Index version 3: `docs` drops `gen` for `sig`, a signature of the
  row's stored fields. `indexed` keeps the generation. Search joins on
  the session alone and reports `indexed.gen`.
- `indexSession` reads the session's current rows by position and
  writes, in one transaction, only rows whose signature changed, new
  rows, and deletes rows past the new end.
- The merge after a pass is forced (`merge -2000`) only when a pass
  deleted rows, until a forced merge finds nothing; after an
  append-only pass it is FTS5's ordinary merge (`merge 2000`).

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

**agent:claude-code/e4a47e8c** at 2026-09-28T16:01:54Z

### Incremental indexing: measurements

This session's transcript (about 16k events), 20 generations each ten
events longer, on this workstation:

| | pass | file after 20 |
|---|---|---|
| main (whole re-index, forced merge) | ~6 s | ~31.9 MiB |
| rows by signature, forced merge every pass | ~0.6 s | ~40 MiB, spiky |
| rows by signature, forced merge only after deletes | 0.5-0.7 s | 23.7 MiB |

Live size is 23.3 MiB (optimize). With incremental writes, a forced
merge after every pass made the file worse: each forced merge rewrites
up to 2000 leaf pages into a new segment even when there is nothing
deleted to drop. FTS5's ordinary merge only merges a level that is full.

### Alternatives considered

- Keep `gen` on rows and copy unchanged rows to the new generation. It
  still rewrites every row in the FTS index, which is the cost this
  removes.
- Detect an append by the normalized file's byte prefix. Re-normalizing
  gives new event ids and ingest times, so the bytes change even when
  the searchable fields do not; comparing the indexed fields catches
  both.
- One transaction per session instead of batches: a session is now
  mostly a no-op. A first index of a large session holds the write lock
  for its duration (about 4 s for 16k events), during which searches
  still read the previous state under WAL.
- The PR #64 CI failure: TestReindexingKeepsTheIndexNearItsLiveSize
  took 324 s under -race and hit the 10-minute limit. Shrunk in #72.

## Summary

Fixed in #63, #72 and #73. search.db opens with incremental auto-vacuum; after a pass that wrote rows it merges up to 2000 pages (forced only after deletes) and returns freed pages. Rows are keyed by position and a signature of their fields (index version 3), so a new generation writes only changed rows: this session's 16k-event transcript re-indexes in about 0.6s instead of 6s, and 20 generations leave the file at 23.7 MiB against 23.3 MiB live (main before #63 levelled near 4x). The index is rebuilt once on the first start of this release.
