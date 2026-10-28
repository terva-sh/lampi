---
schema: 3
id: TKT-01M61MZN8YEFZ90AZDW62QG2DZ
title: Tool usage aggregates over the search index
type: task
status: ready
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
updated_at: 2026-10-28T16:54:05Z
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

- [ ] Calls, results, sessions, error rate, size and latency percentiles are computed per tool and per origin.
- [ ] Results filter by harness, project, model, range and token scope, and bucket by hour or day.
- [ ] The top N largest blocks are listed by session, position, tool and size.
- [ ] Per-request work is bounded and measured on the internal lake.

## Implementation plan

Engine in internal/recall/toolstats.go, `(*Index).ToolStats(ctx, ToolStatsRequest) (ToolStats, error)`.

Scope. search.db cannot see the catalog's bays (no ATTACH), and search drops hits after the query. An aggregate cannot drop rows after GROUP BY, so aggregate per session first: SQL groups by `session_uid, harness, tool_name`, Go keeps a session only when its publication is ready at the indexed generation and it is in `req.Scope` (batch the scope check; cache per request), then merges partials. Mirror how Search filters (search.go:221-255).

Sizes and latency. Exact percentiles over a lake are costly in SQLite, so bucket in SQL: `content_bytes` and latency into log2 buckets (fixed bucket count), merge histograms in Go, report p50/p95 as bucket upper bounds and max exactly. Document the approximation.

Latency pairs call and result rows by `(session_uid, call_id)` with both `recorded_ns` present, result after call, via the index added in TKT-01M61MZMT. Calls with no result count as `unanswered`.

Filters: harness, project, model, since/until on recorded_ns (rows with no recorded time are excluded when a range is given, and counted as `undated` otherwise), tool, origin. Group by tool or origin, optionally by harness or model. Buckets by hour or day using catalog/activity.go's width and range limits.

Largest blocks: top N (max 100) rows by content_bytes, returned as session_uid, pos, harness, tool, origin, bytes, recorded time. No content.

Bounds: a 5s read context like other routes, a cap on distinct groups (reuse CountMaxValues semantics with a `too_many_values` error), and a measurement on a synthetic lake recorded in docs/performance.md.

Tests in internal/recall/toolstats_test.go using openIndex/pass helpers: scope filtering (bay-scoped and session-scoped), generation not ready dropped, histogram merge, latency pairing, orphan calls, time buckets, origin folding.
