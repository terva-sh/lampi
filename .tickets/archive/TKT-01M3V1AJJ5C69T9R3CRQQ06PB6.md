---
schema: 3
id: TKT-01M3V1AJJ5C69T9R3CRQQ06PB6
title: "Search index: reclaim deleted FTS5 rows with a scheduled optimize"
type: task
status: archived
status_reason: null
priority: normal
due_on: null
labels:
  - area/search
assignees: []
milestone: null
parent: null
origin: null
dependencies:
  - TKT-01M3V1AJGSZB7C2C52JCA2SF88
blocks_on: none
references: []
claim: null
archive:
  archived_at: 2026-10-01T06:24:43Z
  from_status: draft
  reason: Duplicate of TKT-01M3NPFNMH
created_at: 2026-10-01T06:09:45Z
updated_at: 2026-10-01T06:24:43Z
created_by:
  id: agent:claude-code/27b21f4b
  name: ""
updated_by:
  id: agent:claude-code/27b21f4b
  name: ""
extensions: {}
---

## Description

The search index's routine merge does not keep up with deleted rows. Rows deleted from an FTS5 index stay in its segments until those segments merge. `reclaim` (`internal/recall/index.go`) forces a merge of at most `mergePages` (2000 pages) after a pass that deleted rows. A lake that re-indexes growing sessions all day deletes faster than that.

### Measured on the internal lake, 2026-10-01

Taken on a copy made with SQLite's online backup, by `storage-v0.5.1-BKPCeV5d/measure.sh`:

| | file | fts_data |
|---|---|---|
| as copied | 2958 MiB | 2102 MiB |
| after `INSERT INTO fts(fts) VALUES('optimize')` | about 2120 MiB | 1263 MiB |

- **Reclaimed:** `optimize` freed 839 MiB of `fts_data` (40%) and took **52 s**.
- **Segment rows:** `fts_data` went from 756,939 rows (counted earlier the same day on the live index) to 330,409.
- **The file column after optimize is computed.** The script printed 2970 MiB because Python's `execute("PRAGMA incremental_vacuum")` steps the pragma only once, which frees one page. A local test confirmed it: one step freed 1 of 2007 free pages, and stepping to completion freed all of them. The Go code frees pages fully, as the live report's 0 free pages shows. So "about 2120 MiB" is 2958 MiB minus 839 MiB.

In comparison, the indexed text is 516 MiB, and `docs` takes 751 MiB.

### Options

1. **Scheduled full optimize.** Run `optimize` when the index has been idle for a while and a measure of deleted data passes a threshold (deleted rows since the last optimize, or the ratio of `fts_data` to text). It holds the write lock for about a minute on this lake, so indexing waits rather than fails.
2. **A larger forced merge per pass.** Cheaper to write, but it rewrites more of the index on every pass, which adds to the write volume the WAL ticket describes.
3. **An operator command** (`serve search optimize`) plus a systemd timer. The simplest option, but it needs an operator to set it up.

Option 1 is the likely choice, with 3 as the manual way to trigger it. Decide after the WAL fix, because optimize writes up to the size of `fts_data` through the WAL in one go.

## Acceptance criteria

- [ ] The index runs a full optimize on its own when deleted data passes a threshold and it is idle, and an operator can trigger one
- [ ] On a copy of a large index, fts_data after the scheduled optimize matches a manual optimize
- [ ] Indexing waits rather than fails while an optimize holds the lock

## Notes

**agent:claude-code/27b21f4b** at 2026-10-01T06:24:42Z

Duplicate of TKT-01M3NPFNMH (Search index: FTS segments hold 0.5 GiB over live size after rebuild), a draft filed 2026-09-28 on the unmerged branch search/wal-limit. This ticket's measurements are copied there.

**agent:claude-code/27b21f4b** at 2026-10-01T06:24:43Z

archived from draft: Duplicate of TKT-01M3NPFNMH
