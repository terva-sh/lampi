---
schema: 3
id: TKT-01M3F2K1RTWH93KB9DSMWR21DQ
title: "Catalog: track published normalization generations explicitly"
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/catalog
  - area/normalize
assignees: []
milestone: null
parent: TKT-01M3F2FSTF28GNEDGQ0XBSZ44W
origin: null
dependencies: []
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

The dashboard cannot call an empty normalize_error successful: pending and never-projected sessions also have no error. Add a nullable successful published generation with a schema migration and generation-safe update. Existing rows remain unknown until a verified publish or explicit safe reconciliation. Use the pending/failed/ready/unknown precedence in docs/web-ui-plan.md.

### Contract and references

Follow docs/web-ui-plan.md, including pinned sibling sources and release A defaults. Use isolated synthetic data. Newly filed work stays draft pending promotion.

## Acceptance criteria

- [ ] Old catalogs migrate without falsely marking legacy rows ready; future schema versions remain refused.
- [ ] Successful matching-generation publication records ready only after output publication.
- [ ] Queued/running/retrying, terminal failure and never-published states classify as pending, failed and unknown respectively.
- [ ] An older worker cannot mark newer work ready; restart, failed publication and purge preserve consistent metadata.
- [ ] Existing normalized JSONL/Parquet and CLI export behavior remain compatible.

## Definition of done

- [ ] Focused tests pass and behavior/contracts are documented; record validation and rationale in the ticket.

## Implementation plan

Read internal/catalog/normalize_queue.go and internal/api/worker.go publication/failure paths. Add the migration and query representation, then mark success only after matching-generation files publish. Keep catalog status consistent across superseded jobs, retry, crash windows, terminal failure and purge. Do not start a full normalization pass merely to backfill status. Add deterministic worker barriers for races and tests for old catalogs/restart; document that ready is a recorded publish and a later missing file is still detected by readers.
