---
schema: 3
id: TKT-01M3V7ZNT0TTQV363HZ0VJ738E
title: "Search index: automerge rewrites old segments inside large transactions"
type: spike
status: done
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
updated_at: 2026-10-01T14:38:16Z
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

## Notes

**agent:claude-code/27b21f4b** at 2026-10-01T14:38:15Z

### Measured, 2026-10-01

**The workload, the same at each setting.** It used this workstation's transcripts and a throwaway probe that is not committed.
- **Rebuild:** every transcript except ten held back, one transaction per session, with a reclaim every 25 sessions.
- **Passes:** 50 of them. Each appended 30 rows to each of the three largest sessions, then ran a reclaim. Every fifth pass also indexed one held-back session whole: the 4th to 13th largest, up to about 7 MiB of text each.
- **Measure:** WAL bytes per transaction, with autocheckpoint off and a truncate after each transaction.

| automerge | rebuild writes | pass writes | worst transaction | worst new session | segments after | file |
|---|---|---|---|---|---|---|
| 4 (default) | 3027 MiB | 428 MiB | 102 MiB | 65 MiB | 10 | 1013 MiB |
| 8 | 2318 MiB | 532 MiB | 90 MiB | 87 MiB | 9 | 1012 MiB |
| 16 | 2163 MiB | 409 MiB | 100 MiB | 77 MiB | 11 | 1013 MiB |

Median search latency over 15 runs was within noise across the three settings, for `error`, `func main`, `journal_size_limit`, a rare id and an absent term (0.3 to 7 ms). The three runs shared the machine, so their timings are comparable only roughly.

**Conclusion: keep automerge at 4.**
- A higher setting saves 23-29% of a full rebuild's writes, and a rebuild is rare.
- It saves nothing over steady passes, and does not lower the worst transaction.
- The earlier single measurement (154 to 52 MiB for one 9 MiB session) depended on which level merges that one session happened to set off, and did not hold over a workload.

**The peak itself** is one session's transaction, at roughly 10-15 times its text. Only splitting a session across transactions would bound it, and that gives up the one-step visibility of a new generation that indexSession guarantees. That was rejected in TKT-01M3NPFNJA. The peak is transient, and since v0.5.2 the WAL is truncated after each pass, so it is documented rather than changed: the WAL bullet in `docs/web-dashboard.md`, Search index.

That section's Size bullet also said the index's merges keep the file near its live size, which TKT-01M3NPFNMH showed is not so. It now says that removed rows stay until a merge meets them, and points to `serve compact`.

## Summary

No change to automerge. Over a rebuild plus 50 passes with new sessions arriving, automerge 8 or 16 saved 23-29% of the rebuild's writes but nothing over passes, and did not lower the worst transaction (65-102 MiB at every setting). Search latency was unchanged. The per-session WAL peak, about 10-15x the session's text, is now documented in docs/web-dashboard.md, together with a corrected Size bullet that points to serve compact.
