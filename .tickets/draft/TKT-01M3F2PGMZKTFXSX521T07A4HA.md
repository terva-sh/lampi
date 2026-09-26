---
schema: 3
id: TKT-01M3F2PGMZKTFXSX521T07A4HA
title: "Web: add filtered transcript search and result navigation"
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/search
  - area/server
assignees: []
milestone: null
parent: TKT-01M3F2PGA1EFCEPEBPT1JFR3JJ
origin: null
dependencies:
  - TKT-01M3F2PGHM6VHQBNE5XKDXS407
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

Expose viewer-authorized search over the FTS5 query layer and an accessible search page. Support literal query text, harness/project/normalization filters and UTC recorded-time range as documented by release B. Show index lag explicitly and link hits to the matching session/generation/event. A changed generation prompts reload rather than navigating to the wrong event.

### Contract

Follow docs/web-ui-plan.md release B and its pinned sibling references. Use only isolated synthetic data. New work remains draft pending promotion.

## Acceptance criteria

- [ ] Viewer can search literal text with supported filters and bounded paginated results; missing timestamps have documented date-filter behavior.
- [ ] Search shows lag/failure coverage and does not present stale, failed or purged content as current.
- [ ] Result links identify generation/event and handle changed sessions with reload guidance.
- [ ] Malicious snippets render as plain text; invalid queries/dates/cursors fail safely and device-only requests cannot access search.
- [ ] Explicit session selection is retained for export without selecting hidden/unbounded results.

## Definition of done

- [ ] Focused tests and relevant API/operator docs are complete; record evidence and decisions in the ticket.

## Implementation plan

Add /api/web/v1/search and a GET search form/page. Default 50/max 200 results, query length capped at 1024 bytes; use bounded plain-text snippets, no HTML from FTS or transcript sources. Bind cursor to query, filters and index generation/version semantics; document that changing data may require a restart of results. Empty query is a validation error rather than a corpus dump. Keep missing recorded timestamps outside date-filtered results and explain the filter in UI. Support explicit selection of session UIDs for the later export screen without implying every search hit will download. Test role guards, literal punctuation, invalid dates/cursors, stale links, null dates and keyboard navigation.
