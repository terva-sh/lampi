---
schema: 3
id: TKT-01M61MZN8YEFZ90AZDW62QG2DZ
title: Tool usage aggregates over the search index
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/search
assignees: []
milestone: null
parent: TKT-01M61MYZ6H6J8MGNPWHS8MTJ1X
origin: null
dependencies:
  - TKT-01M61MZMR6EHF8BAC5F4VNK9VN
  - TKT-01M61MZMTSK36M0TB5ZZFF66RA
  - TKT-01M61MZN3QPKF04MAN3TWE5K0F
  - TKT-01M61PSX6JF6TP9HE9BXEWMMV6
  - TKT-01M61PT07YZSB6SM2EGT50NSJV
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-28T16:20:09Z
updated_at: 2026-10-28T18:18:52Z
created_by:
  id: agent:claude-code/049834bc
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/049834bc
  name: Claude Code local agent
extensions: {}
---

## Description

Aggregate tool use over the search index, so the lake can say which tools are used most, when, and at what cost.

Per tool and per origin (MCP server, plugin, built-in), within filters for harness, project, model, bay scope and a time range:

- calls, results, sessions that used it
- error count and rate, where `is_error` is known
- input and output size: total, p50, p95, max, and the share of a session's tool result bytes
- call-to-result latency p50, p95, max, where both events have harness timestamps
- counts bucketed by hour or day, following the bucket rules in internal/catalog/activity.go
- the top N largest single blocks, as session, position, tool and size

Percentiles over SQLite need care at lake scale; measure on the internal lake and bound the work per request like the existing count limits.

## Acceptance criteria

- [x] Calls, results, sessions, error rate, size and latency percentiles are computed per tool and per origin.
- [x] Results filter by harness, project, model, range and token scope, and bucket by hour or day.
- [x] The top N largest blocks are listed by session, position, tool and size.
- [ ] Per-request work is bounded and measured on the internal lake.

## Implementation plan

Engine in internal/recall/toolstats.go, `(*Index).ToolStats(ctx, ToolStatsRequest) (ToolStats, error)`. Revised from the coordinator's plan after measuring; the note of 2026-10-28 records why.

Scope. `catalog.ReadySessions(ctx, scope)` lists every ready session in scope with its published generation, harness, project and last-accepted time. Go applies the harness and project filters and the `since` prune, and the list goes into a temporary table on the request's connection (`temp.tool_scope`, numbered). The one query joins it and `indexed` on generation, so a stale, pending, failed, purged or out-of-scope session adds nothing.

One pass. The query walks each listed session's tool rows (and usage rows for the context hint) in position order through docs_session. Go pairs results with calls by call id per session, drops Codex event_msg duplicates of a response_item, resolves a result's name and model from its call, and fills counts, histograms, session shares, buckets, the largest blocks and the context hint in that one pass.

Percentiles. Quarter-octave histograms (four buckets per power of two), nearest rank, reported as the bucket's upper bound capped at the exact max: at most 19% high. Totals, maxima and session shares are exact.

Latency. Call to result where both have a recorded time and the result is not earlier. Tools that wait on a person (Claude AskUserQuestion, ExitPlanMode; Codex request_user_input) are left out and flagged. Codex exec cells are flagged code_mode_cell. A Codex inner pair recorded at one instant (no started_at_ms) has no latency.

Inner calls. Codex raw_type item_completed on tool_call/tool_result. Groups carry `inner`, and result_share and session_share are taken within the class (context or inner), so no byte counts twice. Totals report context_result_bytes and inner_result_bytes apart.

Ranges. Rows in [since, until) count; undated rows count only without a range. All rows of a walked session are read so pairs cross the range edge. With since, sessions last accepted more than 24 h before it are skipped.

Bounds. ToolStatsMaxRows (2.5 M rows) and ToolStatsMaxGroups (10,000 keys) fail with ErrTooManyValues; Largest is at most 100; the routes' 5 s read context applies. Measured in docs/performance.md.

Tests in internal/recall/toolstats_test.go; scale measurement in toolstats_scale_test.go behind LAMPI_TOOLSTATS_SCALE.

## Notes

**agent:claude-code/049834bc-stats** at 2026-10-28T17:42:13Z

Implemented in 50c2d87 (engine, catalog.ReadySessions, tests, docs/performance.md). Origin parsing is in internal/toolorigin (605c7c9).

### Deviation from the plan, and why

The plan grouped in SQL per session and merged in Go. I built that first, as GROUP BY over (harness, tool, inner, model, type, size bucket, latency bucket) with correlated lookups through docs_call for pairing, unanswered calls and Codex duplicates. On 200 synthetic sessions (129,646 tool rows) it took 1.2 s for the counts plus 0.4 s for session counts and shares. SQLite sorts every row for GROUP BY, and the correlated lookups cost more than the walk. Without `INDEXED BY docs_call`, the planner (search.db has no ANALYZE statistics) walked whole sessions through docs_session for each lookup.

What replaced it: one walk per session in position order, with Go doing the aggregation. It takes 0.35 s on the same fixture, and every part of the answer comes from that one read. Per-session SQL partials lost too, since histogram rows per session × tool × bucket would number in the hundreds of thousands.

For scope, a temporary table on the request's connection replaced dropping rows after SQL. It costs one catalog query (`catalog.ReadySessions`, which takes a Scope, so TestEverySessionReadTakesAScope passes) and a temp-db insert, and takes no lock on search.db.

For ranges, I tried finding active sessions through docs_call_time and dropped it: on wide ranges its table lookups cost as much as the walk, adding 1.2 s on 30 days at 2,000 sessions. A session the lake last accepted more than 24 h before `since` is skipped instead (`toolStatsClockSlack`), because a harness records a row before the lake receives it.

### Engine API for the surfaces agent (TKT-01M61MZNC, TKT-01M61MZNF)

`(*recall.Index).ToolStats(ctx, recall.ToolStatsRequest) (recall.ToolStats, error)`. Errors: `*recall.FilterError{Param}`, which matches `recall.ErrInvalid` and names the query parameter (harness, project, model, tool, origin, origin_name, group_by, split, bucket, largest, until), and `recall.ErrTooManyValues` (map to `too_many_values`).

Request fields: Scope (required; the zero Scope reads nothing), Harness, Project, Unlinked, Model, Since and Until (*time.Time; Since inclusive, Until exclusive), Tool, OriginKind (toolorigin.Kinds), OriginName, GroupBy ("tool" by default, or "origin"), Split ("", "harness" or "model"), Bucket ("hour" or "day", resolved by catalog.ActivityRequest.Resolve, which overwrites Since and Until with the aligned range), Largest (0..100), Context (bool), and Now (for tests).

Every type is JSON-tagged, so the response serializes as is:

```
{as_of, group_by, split?, since, until, bucket?, bucket_starts?[],
 totals: {calls, results, sessions, unanswered, undated, context_result_bytes, inner_result_bytes, buckets?[]},
 groups: [{tool?, origin: {kind, name?, server?}, harness?, model?, inner,
           calls, results, sessions, unanswered, undated, errors, results_with_error_flag, error_rate|null,
           input_bytes: {count,total,p50,p95,max}, result_bytes: {...},
           result_share, session_share: {sessions,p50,p95,max}|null,
           latency_ms: {count,p50,p95,max}|null, latency_note?: waits_on_person|code_mode_cell, buckets?[]}],
 largest: [{session_uid, generation, position, link, harness, tool, origin, event_type, inner, bytes, recorded_at}],
 context?: {...}, units: {...}, coverage}
```

Groups are sorted by calls, then results, then result bytes. `coverage` is set only for a scope that reads every bay, as search does. For the routes: use a 5 s read context, and consider a default range such as the last 7 days. The 3-day request took 0.25 s at 2,000 sessions; the whole lake took 2.3 s.

### Dependency gap

TKT-01M61PSX6J (Codex inner calls) and TKT-01M61PT07Y (Cursor CLI tool parts) are on the other agent's branch (88eba3e) and are not merged here. The engine follows their final contract as the coordinator gave it: inner rows are harness codex, raw_type exactly `item_completed`, event_type tool_call or tool_result; inner MCP calls are named `mcp__<server>__<tool>`; Cursor CLI parts carry no timestamps, so they show no latency. The tests use fixtures in that shape. No test has run against real projections from that branch. Re-run `go test ./internal/recall/` after the merge.

### AC 4

Per-request work is bounded: ToolStatsMaxRows is 2.5 M rows, ToolStatsMaxGroups 10,000 keys, Largest at most 100, and both limits are tested. It was measured on a synthetic lake of 2,000 sessions and 1.3 M tool rows (docs/performance.md), not on the internal lake, which this session cannot read. Left unticked until someone runs `TestToolStatsScale`'s requests against the internal lake, or times the route there.

**agent:claude-code/049834bc** at 2026-10-28T18:18:52Z

Review 2905 on PR #221 (two medium findings) fixed, commit 1638856. (1) The 24 h accepted-time skip could drop rows recorded in range. Index version 6 stores first and last recorded ns per session in indexed, and the walk filters on them in its join; tested by TestToolStatsRangeReadsRecordedTimes. (2) A paired result now counts under its call's name; tested by TestToolStatsCodexResultTakesItsCallsName. Scale fixture re-measured: last 3 days 161 to 169 ms (was 245 to 321 ms), other requests unchanged.

## Summary

(*recall.Index).ToolStats in internal/recall/toolstats.go (50c2d87) aggregates calls, results, sessions, errors, size and latency percentiles, session shares, hour or day buckets and the largest blocks per tool or origin. It filters by harness, project, model, range, tool, origin and token scope, in one walk per ready in-scope session that catalog.ReadySessions lists, and is bounded by 2.5 M rows and 10,000 keys. On a synthetic lake of 2,000 sessions and 1.3 M rows a whole-lake request takes about 2.3 s, and a 3-day one 0.25 s (docs/performance.md). Not yet measured on the internal lake, and not yet tested against the projections from TKT-01M61PSX and TKT-01M61PT07Y.
