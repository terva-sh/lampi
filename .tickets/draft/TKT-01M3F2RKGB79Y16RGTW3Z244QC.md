---
schema: 3
id: TKT-01M3F2RKGB79Y16RGTW3Z244QC
title: "Catalog: record idempotent accepted head-update history"
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/catalog
  - area/protocol
assignees: []
milestone: null
parent: TKT-01M3F2RKCZZNB6C1EGEG1FDCQH
origin: null
dependencies:
  - TKT-01M3F2FSTF28GNEDGQ0XBSZ44W
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

Add prospective history for accepted session head changes in the same catalog transaction that records the change. Persist an independent coverage-start marker. Do not infer historic events from sessions.ingested_at or provenance. A retry that is unchanged or stale produces no new event. Record metadata only; no transcript content, secrets or auth identifiers.

### Contract

Follow docs/web-ui-plan.md release C. Use isolated synthetic data; this work needs no hosted endpoint or production credentials. New work remains draft pending owner promotion.

## Acceptance criteria

- [ ] Every committed initial/new head has one matching metadata history event; transaction failure produces neither.
- [ ] Unchanged/stale retries, provenance-only additions and divergent copies without a head change add no event; legitimate repeated digest transitions remain recordable.
- [ ] Old/new logical sizes and receipt times support net-change measures without claiming network or physical disk bytes.
- [ ] Migration starts prospective coverage explicitly and never invents events; backup/restore preserves coverage/history and purge removes the session history.
- [ ] Tests prove idempotency, rollback, append/replacement, multi-machine behavior and indexed bounded reads.

## Definition of done

- [ ] Validation evidence and rationale are recorded; measurement and recovery behavior are documented.

## Implementation plan

Inspect internal/catalog/catalog.go Ingest decisions and head update transaction. Migrate a history table with event key/order, session UID, machine ID, harness, UTC receipt time, old/new digest, old/new logical head size and relation; first head has old size zero. Record one event on initial acceptance or a changed head, not a new alias/provenance-only observation or divergent_copy that leaves head unchanged. Support a legitimate later return to an earlier digest: uniqueness must not suppress a real head transition. Rollback drops both head mutation and event. Store coverage start once, retain it through backup/restore, and remove session history during purge without adding a TTL. Add indexes for time/harness buckets, parameterized readers and fixtures for append, replace, stale retry, conflicts and second-machine CAS reuse.
