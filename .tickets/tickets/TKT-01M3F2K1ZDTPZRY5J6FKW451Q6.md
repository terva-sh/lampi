---
schema: 3
id: TKT-01M3F2K1ZDTPZRY5J6FKW451Q6
title: "Catalog: add bounded dashboard queries and stable pagination"
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/catalog
assignees: []
milestone: null
parent: TKT-01M3F2FSTF28GNEDGQ0XBSZ44W
origin: null
dependencies:
  - TKT-01M3F2K1RTWH93KB9DSMWR21DQ
blocks_on: none
references:
  - ref: plan:web-ui
    path: docs/web-ui-plan.md
claim: null
archive: null
created_at: 2026-09-26T14:40:58Z
updated_at: 2026-09-26T14:51:23Z
created_by:
  id: agent:codex/web-ui-planning
  name: ""
updated_by:
  id: agent:codex/web-ui-release-a
  name: ""
extensions: {}
---

## Description

### Scope and rationale

Build metadata-only overview, session listing/detail and conflict queries for release A. Counts use a consistent read snapshot; sessions are grouped by harness and distinct provenance machine IDs. Do not scan CAS or normalized files. Use the fields, state semantics, filter rules and cursor contracts in docs/web-ui-plan.md. Bound nested artifact history and provenance as well as top-level lists.

### Contract and references

Follow docs/web-ui-plan.md, including pinned sibling sources and release A defaults. Use isolated synthetic data. Newly filed work stays draft pending promotion.

## Acceptance criteria

- [ ] Overview counts, harness breakdown and normalization states match fixture catalog data without reading transcript/CAS files.
- [ ] Session filters and bounded cursor pages use deterministic chronological ordering with no omissions/duplicates in an unchanged catalog.
- [ ] Every returned collection, including details/history/provenance/conflicts, has a bounded query or pagination contract.
- [ ] 20k-session query-plan checks show indexed list pagination; counts avoid per-session queries and response sizes stay bounded.
- [ ] Queries remain cancellation-aware and read-only beside active ingestion.

## Definition of done

- [ ] Focused tests pass and behavior/contracts are documented; record validation and rationale in the ticket.

## Implementation plan

Add dedicated catalog DTOs and parameterized SQL rather than extending unbounded ListSessions for browser use. Index supported filters and newest-head-update/UID ordering; handle legacy mixed-precision RFC3339 timestamps chronologically. Default limit 50/max 200, keyset cursors bound to filters, explicit unlinked-project selector. Test empty catalogs, same-time rows, fractional timestamps, multi-machine dedup, unknown project, every status and invalid filters/cursors. Seed 20k rows to inspect query plans and page bounds; document live-view behavior on concurrent head movement.
