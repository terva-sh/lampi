---
schema: 3
id: TKT-01M4EPDBG32RKNM9D5MPZJQ25F
title: "Dashboard: full pagination and Operations maintenance controls"
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-08T21:23:50Z
updated_at: 2026-10-08T21:24:12Z
created_by:
  id: agent:codex/t3code-8afe4a1e
  name: ""
updated_by:
  id: agent:codex/t3code-8afe4a1e
  name: ""
extensions: {}
---

## Description

User requested first, previous, nearby page numbers, next and last controls above and below every paginated list, plus Operations controls for compaction and cleanup.

## Acceptance criteria

- [ ] All existing paginated dashboard lists have matching top and bottom controls with nearby page numbers and first/last navigation.
- [ ] Pagination preserves filters, limits, scope and transcript generation checks.
- [ ] Operations offers guarded maintenance requests with progress and results using existing lake routines.

## Implementation plan

Add a shared accessible pager backed by lightweight scoped cursor-boundary queries, caching transcript boundaries by snapshot and page size. Preserve API cursors and URL filters. Add admin-only asynchronous search-index compaction, stale-upload cleanup and storage sampling through existing routines; keep CAS version folding offline because Compact requires the exclusive lake lock and no concurrent ingest. Verify page boundaries, scope, generation changes, maintenance authorization, CSRF, concurrency and result reporting.
