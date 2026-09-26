---
schema: 3
id: TKT-01M3F2RKKRM1MP6GJ0P5BJ3JW7
title: "Web analytics: add bounded ingestion charts and release validation"
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
  - area/catalog
  - area/ci
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
updated_at: 2026-09-26T14:44:00Z
created_by:
  id: agent:codex/web-ui-planning
  name: ""
updated_by:
  id: agent:codex/web-ui-planning
  name: ""
extensions: {}
---

## Description

### Scope and rationale

Add viewer-only ingestion charts from accepted head-update history. Use UTC hourly or daily buckets, harness filtering and a maximum 90-day query range. Display accepted updates and net logical head-size change as separate series. Show coverage start, zero buckets within coverage, unavailable time before coverage, and that purging sessions also removes their history. This ticket closes release C with API/UI tests and operator documentation.

### Contract

Follow docs/web-ui-plan.md release C. Use isolated synthetic data; this work needs no hosted endpoint or production credentials. New work remains draft pending owner promotion.

## Acceptance criteria

- [ ] Viewer API/UI show correct UTC hourly/daily accepted-update counts and signed net logical-size changes with harness filters.
- [ ] Invalid/unbounded requests fail; default/max ranges and output bounds are enforced using indexed aggregate queries.
- [ ] Charts distinguish zero from unmeasured history and explain coverage/purge limitations without calling these bytes network throughput or disk growth.
- [ ] Synthetic ingestion-to-chart, retry, purge and backup/restore tests plus accessible browser smoke verify the complete feature.
- [ ] make ci and go test -race ./... pass; architecture/browser API/operator docs define units and coverage.

## Definition of done

- [ ] Validation evidence and rationale are recorded; measurement and recovery behavior are documented.

## Implementation plan

Implement /api/web/v1/activity with validated RFC3339 time bounds, hourly/daily bucket enum, UTC alignment, a deterministic as_of, default last seven days daily and capped output. Sum new minus old logical sizes and preserve negative changes. Use SQL aggregation and the history indexes; do not scan CAS/JSONL. Add accessible charts and equivalent tables to the existing dashboard, reusing visibility-aware refresh and auth guards. Test boundary timestamps, daylight-saving dates using UTC, no events, newly enabled coverage, purged rows, rejected ranges and seeded 90-day data. Run integrated ingest-to-chart and backup/restore tests, make ci and go test -race ./..., then document the measurement definitions and known omissions.
