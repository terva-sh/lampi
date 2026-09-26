---
schema: 3
id: TKT-01M3F2K1RTWH93KB9DSMWR21DQ
title: "Catalog: track published normalization generations explicitly"
type: task
status: done
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
updated_at: 2026-09-26T15:02:10Z
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

- [x] Old catalogs migrate without falsely marking legacy rows ready; future schema versions remain refused.
- [x] Successful matching-generation publication records ready only after output publication.
- [x] Queued/running/retrying, terminal failure and never-published states classify as pending, failed and unknown respectively.
- [x] An older worker cannot mark newer work ready; restart, failed publication and purge preserve consistent metadata.
- [x] Existing normalized JSONL/Parquet and CLI export behavior remain compatible.

## Definition of done

- [x] Focused tests pass and behavior/contracts are documented; record validation and rationale in the ticket.

## Implementation plan

Read internal/catalog/normalize_queue.go and internal/api/worker.go publication/failure paths. Add the migration and query representation, then mark success only after matching-generation files publish. Keep catalog status consistent across superseded jobs, retry, crash windows, terminal failure and purge. Do not start a full normalization pass merely to backfill status. Add deterministic worker barriers for races and tests for old catalogs/restart; document that ready is a recorded publish and a later missing file is still detected by readers.

## Summary

Added nullable published generation and head digest in an append-only catalog migration. Successful JSONL and Parquet publication conditionally records the exact generation/head; failures invalidate success. State precedence is pending, failed, ready, unknown, and legacy rows remain unknown. The extra head digest closes the existing ingest-commit/enqueue interval: generation alone can still name the prior transcript in that interval. Tests cover legacy migration, restart, stale generation/head, errors/retry and real worker publication. Catalog/API/CLI tests pass; focused migration/publication tests rerun after assertions were added. Existing CLI export and output schemas are unchanged.
