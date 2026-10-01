---
schema: 3
id: TKT-01M3V7ZNT0TTQV363HZ0VJ738E
title: "Search index: automerge rewrites old segments inside large transactions"
type: spike
status: draft
status_reason: null
priority: low
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
created_at: 2026-10-01T08:06:08Z
updated_at: 2026-10-01T08:06:08Z
created_by:
  id: agent:claude-code/27b21f4b
  name: ""
updated_by:
  id: agent:claude-code/27b21f4b
  name: ""
extensions: {}
---

## Description

The ~300 MiB WAL peak seen on the internal lake on 2026-10-01 comes from indexing one large session in one transaction. FTS5's automerge (default 4) rewrites existing segments inside that transaction. Measured on a 962 MiB index built from this workstation's transcripts (TKT-01M3NPFNMH, findings note):

| new session text | WAL, automerge 4 | automerge 8 | automerge 0 |
|---|---|---|---|
| 11 MiB | 113 MiB | 55 MiB | 53 MiB |
| 9 MiB | 154 MiB | 52 MiB | 77 MiB |
| 7 MiB | 115 MiB | 44 MiB | 34 MiB |

Every WAL frame was a distinct page, so this is not page-cache spill: cache sizes of 2, 32 and 128 MiB gave identical results.

A higher automerge leaves more segments per level. Queries read more segments, and reclaim's bounded merges take on more of the work. Before changing it, measure:
- total pages written across a rebuild plus a day of passes, at automerge 4, 8 and 16;
- search latency for a few representative queries at each setting, against the same index merged with `optimize`;
- whether reclaim's `merge` steps keep the segment count bounded.

automerge is stored in the index (`fts_config`), so a change applies to existing indexes when set at open.
