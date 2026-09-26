---
schema: 3
id: TKT-01M3F2RKCZZNB6C1EGEG1FDCQH
title: "Lake analytics: record and visualize accepted ingestion updates"
type: epic
status: draft
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

### Outcome and rationale

Release C of docs/web-ui-plan.md adds trustworthy ingestion history and charts. Existing session timestamps describe the latest head, and provenance is deduplicated by session/machine/digest, so neither can reconstruct throughput. Record accepted head updates prospectively and describe them honestly; never fabricate historical rates from current rows.

### Scope and scheduling

Depends on the OIDC dashboard release, not retrieval: these measurements use catalog metadata and have no dependency on transcript content, FTS or export. It can be scheduled independently of release B after owner promotion. Measure accepted updates and net logical head-size change, not network bytes, physical CAS growth, online agents or token/cost usage. No TTL, external metrics stack or infrastructure provisioning. The two children own recording and the complete query/UI/validation slice. Follow the linked design and use synthetic data only.

## Acceptance criteria

- [ ] Accepted head updates have durable idempotent history and an explicit measurement coverage boundary.
- [ ] Authorized viewers can inspect bounded time-series charts with accurate units, empty states and purge limitations.
- [ ] Migration, ingest retry, purge, backup/restore and aggregate correctness are validated without live data.
