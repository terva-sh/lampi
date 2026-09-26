---
schema: 3
id: TKT-01M3F2PGHM6VHQBNE5XKDXS407
title: "Search: index current normalized content with durable FTS5 work"
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/search
  - area/catalog
assignees: []
milestone: null
parent: TKT-01M3F2PGA1EFCEPEBPT1JFR3JJ
origin: null
dependencies:
  - TKT-01M3F2PGDY06D7XE12NWQ9EZF4
blocks_on: none
references:
  - ref: plan:web-ui
    path: docs/web-ui-plan.md
claim: null
archive: null
created_at: 2026-09-26T14:42:52Z
updated_at: 2026-09-26T14:42:52Z
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

Create a rebuildable SQLite FTS5 search index for current normalized content_text. Index keys include session UID, generation and event position. Indexing must lag ingestion safely, expose pending/failed state and never make stale results look current. Keep unknown/encrypted fields out of indexed text. This ticket owns index schema, durable jobs, maintenance and query functions, not the browser search screen.

### Contract

Follow docs/web-ui-plan.md release B and its pinned sibling references. Use only isolated synthetic data. New work remains draft pending promotion.

## Acceptance criteria

- [ ] Durable indexing converges after restart and superseded work cannot publish stale rows; failures leave ingestion functional and lag visible.
- [ ] Only current successful normalized generations and content_text are searchable; purged and failed generations are excluded at query time.
- [ ] Literal query/filter/date input is validated and result pagination is bounded, deterministic and resistant to SQL/FTS injection.
- [ ] Purge, rebuild and backup/restore tests preserve index correctness without concurrent writer violations.
- [ ] Indexing and queries have bounded memory/results on large synthetic sessions and report pending/failed coverage.

## Definition of done

- [ ] Focused tests and relevant API/operator docs are complete; record evidence and decisions in the ticket.

## Implementation plan

Use the generation-safe reader and catalog migrations. Queue index work durably after successful normalized publication with restart reconciliation for the crash window; publish replacement index rows transactionally only if generation is still current. A superseded/failing job cannot corrupt the new index or fail ingestion. Chunk work and bound memory for long sessions. Add a documented server-owned rebuild operation that respects lake.lock and does not write alongside another publisher. Integrate purge and backup/restore, including old catalogs without an index. Add parameterized literal-text query functions with bounded results, supported session filters and UTC recorded-time ranges; no user SQL or FTS syntax. Test retries, crashes, updates, deletion, unindexed/unknown dates and 20k synthetic sessions.
