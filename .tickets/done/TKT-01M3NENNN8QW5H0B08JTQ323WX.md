---
schema: 3
id: TKT-01M3NENNN8QW5H0B08JTQ323WX
title: Search index rewrites untimed events at every sync
type: bug
status: done
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
claim: null
archive: null
created_at: 2026-09-29T02:07:34Z
updated_at: 2026-09-29T02:54:48Z
created_by:
  id: agent:claude-code/fbqx
  name: ""
updated_by:
  id: agent:claude-code/fbqx
  name: ""
extensions: {}
---

## Description

The hosted search index measured 4.9 GB beside 1.57 GB of normalized events. On 2026-09-29 a one-shot build from 329 local Claude Code sessions (800 MiB raw, 1.09 GB normalized) came to 611 MiB. That points to about 0.9 GB for the hosted lake, so the hosted index is five to six times its live size.

Cause: when a source line has no timestamp, projection falls back to the projection time for `recorded_at` (`normalize.recordedAt`). Claude Code writes many such lines (`ai-title`, `last-prompt`, `atis-latch` and others). Each generation stamps them anew, so their index signature changes, and the index deletes and reinserts every one at every sync.

A simulation published the 40 largest sessions as 12 growing prefixes. About 20% of each session's earlier rows changed signature at every generation, and 506k row ids were spent for 254k rows. The deleted rows stay in FTS5 segments until a merge, and each pass merges at most 2000 pages. So on a lake that syncs all day, the churn outruns the merge.

Fix: the index treats `recorded_at` equal to `ingested_at` as no time, as projection writes it only when the source had none. Search then reports `recorded_at` as null, which docs/web-api.md already says it does for such events. Bump the index version, so the bloated file is rebuilt once on upgrade.

## Acceptance criteria

- [x] Rows whose source had no timestamp keep their signature across generations.
- [x] The index is rebuilt once on upgrade, reclaiming the existing bloat.

## Notes

**agent:claude-code/fbqx** at 2026-09-29T02:15:16Z

Fixed on branch search/untimed-rows.
- `docRow.fill` now takes `recorded_at` equal to `ingested_at` as no time.
- The index version is now 4, so search.db is rebuilt once on upgrade.

Verification:
- TestUntimedEventsAreNotRewrittenEachGeneration fails without the fix.
- The same 40-session, 12-generation simulation with the fix: no earlier row changed signature, and 254,696 row ids were used for 254,696 rows (506k before the fix). The file ended at 393 MiB against 390 MiB compact.

Alternatives considered:
- Stop projection from falling back to the projection time. That would change the normalized schema (`recorded_at` is a required string) and every derived file, as well as parquet partitioning by date. That is a larger decision for the owner.
- Merge harder after every pass. That treats the symptom, and costs a rewrite of up to 8 MiB per pass (TKT-01M3KC2DD).

## Summary

Fixed in #128.
- The search index takes recorded_at equal to ingested_at as no time. Projection writes that only when the source line had none, so such rows keep their signature across generations, and search reports recorded_at as null for them.
- Index version 4 rebuilds search.db once on the first start of the release, which reclaims the existing bloat. The hosted lake measured 4.9 GB, where about 0.9 GB is expected.
- A simulation of 40 real sessions over 12 generations wrote 254,696 row ids for 254,696 rows, against 506k before the fix. The file ended at 393 MiB against 390 MiB compact.
- TestUntimedEventsAreNotRewrittenEachGeneration covers it.
