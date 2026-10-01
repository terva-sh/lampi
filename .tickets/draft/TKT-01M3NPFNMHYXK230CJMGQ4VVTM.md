---
schema: 3
id: TKT-01M3NPFNMHYXK230CJMGQ4VVTM
title: "Search index: FTS segments hold 0.5 GiB over live size after rebuild"
type: task
status: draft
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
created_at: 2026-09-29T04:24:06Z
updated_at: 2026-10-01T07:35:32Z
created_by:
  id: agent:claude-code/65ab7244
  name: ""
updated_by:
  id: agent:claude-code/27b21f4b
  name: ""
extensions: {}
---

## Description

The live size of the dev lake's search index is well below what is on disk once the WAL is set aside (the WAL is TKT-01M3NPFNJA, Search index WAL keeps its peak size).

A `VACUUM INTO` copy taken at 2026-09-29 04:17 UTC, about 55 minutes after the index-version-4 rebuild began:

| | before | after `optimize` + `VACUUM` |
|---|---|---|
| file | 2.21 GiB | 1.67 GiB |
| `fts_data` | 1,588 MiB, 425,815 rows | 1,029 MiB, 269,628 rows |
| `docs` | 609.5 MiB | 609.5 MiB |

`optimize` took 34 s on this workstation. Event text is 424 MiB over 713,425 rows.

Row ids reached 746,531 for 713,425 rows, so about 33k rows had been replaced since the rebuild. Every pass that deletes a row sets `deleted`, and the next reclaim is then a forced merge of 2000 pages. The index may therefore work its way down to its live size by itself. It may also not: new small segments keep arriving from the syncs of five active sessions.

To do:
- Measure the file over a few hours on the dev lake, and check whether the forced merges converge.
- If they do not, run a merge to completion after a pass that wrote a large share of the index, such as a rebuild. That can be spread over passes as a forced merge, or done as one `optimize`. `optimize` costs a transaction the size of the FTS index, about 1 GiB of WAL here.
- Find what still replaces rows after TKT-01M3NENNN8. Compare signatures of one session across two generations.

## Notes

**agent:claude-code/27b21f4b** at 2026-10-01T06:24:58Z

### Answered on the internal lake, 2026-10-01: the forced merges do not converge

Measured on a copy by `storage-v0.5.1-BKPCeV5d/measure.sh`, two days after the 09-29 measurement above:

| | fts_data | file |
|---|---|---|
| as copied | 2102 MiB | 2958 MiB |
| after `optimize` | 1263 MiB (-40%) | about 2120 MiB |

- **optimize** took 52 s.
- **Segment rows** went from 756,939 to 330,409.
- **Indexed text** is 516 MiB over 903,407 rows, and `docs` takes 751 MiB.
- **The file after optimize is computed**, as 2958 MiB minus 839 MiB. The script's Python ran `PRAGMA incremental_vacuum` for one step only, which frees one page. A local test confirmed that.

The dead share grew from about 35% on 09-29 to 40%, so the 2000-page forced merge after each pass does not bring the index down to its live size.

**Proposal:** run a full `optimize` when the index has been idle and deleted data has passed a threshold, plus an operator command to run it by hand. It needs the WAL fix (TKT-01M3NPFNJA) first, because one optimize writes a transaction the size of `fts_data` through the WAL.

**Layout measurement, for TKT-01M3K45MX:** the same text with `detail=column` takes 581 MiB of `fts_data` once optimized. Its search behaviour was not tested.

**agent:claude-code/27b21f4b** at 2026-10-01T07:35:32Z

### One pass wrote about 300 MiB of WAL on the live lake, 2026-10-01

v0.5.2 truncates the WAL at the end of each pass (TKT-01M3NPFNJA), so a pass's writes now show up as a transient WAL size. On the internal lake, about three minutes after the v0.5.2 start, the agent had uploaded two growing sessions. `search.db-wal` was then 311397872 bytes, and it was 0 again a few minutes later. `search.db` went from 3131387904 to 3118047232 bytes over the same span.

So one pass that re-indexed a few live sessions wrote about 300 MiB, while the sessions only grew by kilobytes. That is a clue for this ticket's third to-do item: either most of each session's rows are replaced every generation, or FTS5's merges inside the session's transaction rewrite large segments. A transient peak is acceptable disk use. The write volume per pass, every few minutes, is the cost worth finding.
