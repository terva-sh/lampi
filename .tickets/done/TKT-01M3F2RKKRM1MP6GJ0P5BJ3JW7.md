---
schema: 3
id: TKT-01M3F2RKKRM1MP6GJ0P5BJ3JW7
title: "Web API: serve bounded UTC buckets of accepted head updates"
type: task
status: done
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
updated_at: 2026-09-27T23:46:50Z
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

- [x] Viewer API returns correct UTC hourly/daily accepted-update counts and signed net logical-size changes, with a harness filter, for boundary timestamps and ranges that include no events.
- [x] Invalid, repeated, unknown and over-range requests fail with 400; default ranges, alignment, the future clamp and the 14-day hourly and 90-day daily caps are tested.
- [x] Buckets before the coverage marker are none with null counts, the bucket holding it is partial, later buckets are full; a lake with no marker reports every bucket as none.
- [x] docs/web-api.md defines the endpoint, units and coverage semantics.
- [x] The aggregate uses the history indexes (EXPLAIN QUERY PLAN) and a 90-day daily read over seeded synthetic history completes under the 5-second read deadline; through the full mux the endpoint returns 401 without a viewer session (sign-in already refuses accounts outside the viewer groups).

## Definition of done

- [x] Validation evidence and rationale are recorded; measurement and recovery behavior are documented.

## Implementation plan

1. internal/catalog/activity.go: `ActivityRequest` (bucket, from, until, harness), validation returning `ErrPage`, alignment in Go with `time.Time.Truncate` on UTC, and `Activity(ctx, req, now)` running `SELECT received_ns / :width, COUNT(*), SUM(new_size - old_size) FROM head_updates WHERE received_ns >= ? AND received_ns < ? [AND harness = ?] GROUP BY 1` in a read-only transaction together with the coverage marker, then filling the dense bucket list in Go. Unix epoch is a UTC day boundary, so integer division aligns.
2. internal/web/server.go: register `/api/web/v1/activity`; parse with a dedicated parser mirroring `parsePage` (single values, known keys only).
3. Tests: catalog unit tests with a fixed clock and hand-inserted history (boundary nanoseconds, negative net, harness filter, partial bucket, no marker), a seeded 90-day history plan and timing log, and web tests through `New` with the synthetic IdP for 200/400/401/403.
4. docs/web-api.md section for the endpoint.

## Notes

**agent:claude-code/e226d0e4** at 2026-09-27T22:18:29Z

Implementation choices, one correction, and validation.

- Correction to criterion 4 as written during grooming: it asked for a 403 for a non-viewer through the full mux. That case cannot happen. The OIDC callback refuses an identity outside the viewer groups (internal/webauth/browser.go, the `!id.Viewer` check in the callback; covered by `TestUnmappedGroupsAndLimits`), so no session without the viewer role exists to reach Guard's 403 branch. The test covers the reachable case: 401 without a session. The criterion is reworded to match. This supersedes the grooming text; nothing else changed.
- The cap counts buckets after the range is rounded outward. So a caller asking for "90 days before now" at noon gets 400 (91 daily buckets), not a range quietly narrowed. The alternative, checking the span before rounding, would let responses reach 91 or 15 buckets and makes the cap harder to state. The page ticket (TKT-01M3JEKA6) should build its presets from midnight- and hour-aligned ends.
- Bucket index in SQL is `(received_ns - from) / width` with `from` aligned, so SQLite groups without date functions, and the plan stays on the covering indexes (`TestActivityQueryUsesCoveringIndexes`).
- The coverage marker and the aggregate are read in one read-only transaction, so the two agree.
- Empty query values take the defaults, so the HTML form in the page ticket can submit every field.

Validation: `GOFLAGS=-mod=mod just ci` green; `go test -race ./internal/web ./internal/catalog` green. 200,000 synthetic updates over 90 days: the 90-day daily read took about 81 ms, codex-only 28 ms, 14 days hourly 11 ms (TestActivity90DaysWithinReadDeadline logs these, under the 5-second web read deadline; not asserted because timings depend on the machine).

**agent:claude-code/e226d0e4** at 2026-09-27T22:45:22Z

Review round on PR 41. This supersedes nothing in the earlier note; it adds to it.

- terva-review found that a reversed or equal pair of times inside one bucket rounded to that bucket instead of a 400. Fixed in 36f73b4 by checking the given instants before alignment.
- terva-review found that `UnixNano` alignment is undefined outside 1678 to 2262. Fixed in 36f73b4 by refusing times before 1970 or after 2200, rather than widening the arithmetic: the lake holds nothing before 1970.
- A second pass found that `from=0001-01-01T00:00:00Z` parses to Go's zero time, which `Resolve` reads as "not given". Fixed in c7ce659: `parseActivity` refuses any time before 1970. Keeping zero as the sentinel was preferred over pointer fields because every catalog caller is internal, and the HTTP parser is the only place user input enters.
- CI run 415 failed `TestActivity90DaysWithinReadDeadline` under `-race`: the race detector slows the pure-Go SQLite driver past 5 seconds on the runner. Under `-race` the test now checks sums on 20,000 rows with no deadline (build-tagged `raceEnabled`). Runs without `-race` keep 200,000 rows and the deadline, so criterion 4 is still proved by `just ci` and GitHub CI.

State at hand-off: PR 41 CI green, terva-review clean on c7ce659. Not merged; merging is the owner's call.

## Summary

Landed in PR 41. GET /api/web/v1/activity returns hourly or daily UTC buckets of accepted head updates and net logical head-size change, filtered by harness. Every bucket in the range is listed, with coverage none (null counts), partial or full against lake_meta.head_updates_since. Ranges are rounded outward and capped at 14 days of hours or 90 days. Reversed or empty pairs, times before 1970 or after 2200, and unknown or repeated parameters are 400, never quietly narrowed. One aggregate query runs over the covering time indexes: about 80 ms for 90 days of 200,000 updates. Code: internal/catalog/activity.go and internal/web/server.go. Docs: docs/web-api.md#activity. Grooming's 403 criterion was reworded to the reachable 401; see the notes.
