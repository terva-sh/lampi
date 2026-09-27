---
schema: 3
id: TKT-01M3F2RKCZZNB6C1EGEG1FDCQH
title: "Lake analytics: record and visualize accepted ingestion updates"
type: epic
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area/catalog
  - area/server
assignees: []
milestone: null
parent: null
origin: null
dependencies:
  - TKT-01M3F2FSTF28GNEDGQ0XBSZ44W
blocks_on: children
references:
  - ref: plan:web-ui
    path: docs/web-ui-plan.md
claim: null
archive: null
created_at: 2026-09-26T14:44:00Z
updated_at: 2026-09-27T22:09:04Z
created_by:
  id: agent:codex/web-ui-planning
  name: ""
updated_by:
  id: agent:claude-code/e226d0e4
  name: ""
extensions: {}
---

## Description

### Outcome and rationale

Release C of docs/web-ui-plan.md adds trustworthy ingestion history and charts. Existing session timestamps describe the latest head, and provenance is deduplicated by session/machine/digest, so neither can reconstruct throughput. Record accepted head updates prospectively and describe them honestly; never fabricate historical rates from current rows.

### Scope and scheduling

Depends on the OIDC dashboard release, not retrieval: these measurements use catalog metadata and have no dependency on transcript content, FTS or export. Measure accepted updates and net logical head-size change, not network bytes, physical CAS growth, online agents or token/cost usage. No TTL, external metrics stack or infrastructure provisioning. Follow the linked design and use synthetic data only.

### Grooming, 2026-09-28

Groomed against the code on main at 5eaa7bc before promotion.

- The work lands as three tickets, one pull request each: recording (TKT-01M3F2RKG), the activity read API (TKT-01M3F2RKK), and the activity page with release validation (TKT-01M3JEKA6, filed during grooming). The chart ticket was split because terva-review refuses a pull request too large for its context before any model runs, and API plus UI plus docs in one change is that size.
- History stores the old and the new head's logical size. The design said only the new size, but a net change needs both, and reading the old size later from artifact rows fails once purge or a later head rewrite changes them.
- Hourly buckets are capped at a 14-day range (336 buckets); daily keeps the 90-day cap. A 90-day hourly range is 2,160 bars, which is not readable as a chart and not a useful table.
- Charts are server-rendered inline SVG beside an equivalent table, like the harness meters on the overview. They work without JavaScript and are testable from Go.

### Children

1. TKT-01M3F2RKG: Catalog: record idempotent accepted head-update history.
2. TKT-01M3F2RKK: Web API: serve bounded UTC buckets of accepted head updates.
3. TKT-01M3JEKA6: Web UI: activity page with charts, tables and release C validation.

## Acceptance criteria

- [ ] Accepted head updates have durable idempotent history and an explicit measurement coverage boundary.
- [ ] Authorized viewers can inspect bounded time-series charts with accurate units, empty states and purge limitations.
- [ ] Migration, ingest retry, purge, backup/restore and aggregate correctness are validated without live data.
