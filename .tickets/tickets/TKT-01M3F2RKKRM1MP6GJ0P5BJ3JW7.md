---
schema: 3
id: TKT-01M3F2RKKRM1MP6GJ0P5BJ3JW7
title: "Web API: serve bounded UTC buckets of accepted head updates"
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
  - area/catalog
  - area/docs
assignees: []
milestone: null
parent: TKT-01M3F2RKCZZNB6C1EGEG1FDCQH
origin: null
dependencies:
  - TKT-01M3F2RKGB79Y16RGTW3Z244QC
blocks_on: none
references:
  - ref: plan:web-ui
    path: docs/web-ui-plan.md
claim: null
archive: null
created_at: 2026-09-26T14:44:00Z
updated_at: 2026-09-27T22:08:39Z
created_by:
  id: agent:codex/web-ui-planning
  name: ""
updated_by:
  id: agent:claude-code/e226d0e4
  name: ""
extensions: {}
---

## Description

### Scope and rationale

Serve the accepted head-update history (TKT-01M3F2RKG) as bounded UTC time buckets to viewers. Accepted updates and net logical head-size change are separate series. Every bucket in the range is returned, so a reader can tell zero from unmeasured. Split from the original chart ticket during grooming on 2026-09-28 so the API and the page each land as a pull request terva-review can read; the page is the sibling ticket filed then.

### Contract

`GET /api/web/v1/activity`, viewer role, through `guardRead` like the other reads.

- Parameters: `bucket` = `hour` or `day` (default `day`), `from` and `until` as RFC3339 (default: the last 7 whole UTC days through the current day for `day`, the last 24 hours for `hour`), `harness` from the existing harness list. Unknown, repeated or malformed parameters are `400 invalid_filters_or_cursor`, like other reads.
- `from` and `until` are aligned down/up to bucket boundaries in UTC. The aligned range is at most 90 days for `day` and 14 days for `hour`; wider is `400`, not silently clipped. `until` in the future is clamped to the end of the current bucket.
- Response: `bucket`, aligned `from` and `until`, `harness`, `coverage_since` (null when the catalog has no marker), `as_of`, `units` (`updates`: count of accepted head updates; `net_logical_bytes`: sum of new minus old logical head size), and `buckets`: one object per bucket in order with `start`, `coverage` (`none`, `partial`, `full`), `updates` and `net_logical_bytes`. A `none` bucket carries null counts, not zero.
- Totals over covered buckets are included so the page does not re-add them.
- One SQL aggregate over `head_updates` using the `(received_ns)` or `(harness, received_ns)` index; no CAS or JSONL reads.

## Acceptance criteria

- [ ] Viewer API returns correct UTC hourly/daily accepted-update counts and signed net logical-size changes, with a harness filter, for boundary timestamps and ranges that include no events.
- [ ] Invalid, repeated, unknown and over-range requests fail with 400; default ranges, alignment, the future clamp and the 14-day hourly and 90-day daily caps are tested.
- [ ] Buckets before the coverage marker are none with null counts, the bucket holding it is partial, later buckets are full; a lake with no marker reports every bucket as none.
- [ ] The aggregate uses the history indexes (EXPLAIN QUERY PLAN) and a 90-day daily read over seeded synthetic history stays within the 5-second read deadline; auth is exercised through the full mux (401, 403 for a non-viewer).
- [ ] docs/web-api.md defines the endpoint, units and coverage semantics.

## Definition of done

- [ ] Validation evidence and rationale are recorded; measurement and recovery behavior are documented.

## Implementation plan

1. internal/catalog/activity.go: `ActivityRequest` (bucket, from, until, harness), validation returning `ErrPage`, alignment in Go with `time.Time.Truncate` on UTC, and `Activity(ctx, req, now)` running `SELECT received_ns / :width, COUNT(*), SUM(new_size - old_size) FROM head_updates WHERE received_ns >= ? AND received_ns < ? [AND harness = ?] GROUP BY 1` in a read-only transaction together with the coverage marker, then filling the dense bucket list in Go. Unix epoch is a UTC day boundary, so integer division aligns.
2. internal/web/server.go: register `/api/web/v1/activity`; parse with a dedicated parser mirroring `parsePage` (single values, known keys only).
3. Tests: catalog unit tests with a fixed clock and hand-inserted history (boundary nanoseconds, negative net, harness filter, partial bucket, no marker), a seeded 90-day history plan and timing log, and web tests through `New` with the synthetic IdP for 200/400/401/403.
4. docs/web-api.md section for the endpoint.
