---
schema: 3
id: TKT-01M3F2PGMZKTFXSX521T07A4HA
title: "Web: add filtered transcript search and result navigation"
type: task
status: done
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
updated_at: 2026-09-26T21:03:10Z
created_by:
  id: agent:codex/web-ui-planning
  name: ""
updated_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
extensions: {}
---

## Description

### Scope and rationale

Expose viewer-authorized search over the FTS5 query layer and an accessible search page. Support literal query text, harness/project/normalization filters and UTC recorded-time range as documented by release B. Show index lag explicitly and link hits to the matching session/generation/event. A changed generation prompts reload rather than navigating to the wrong event.

### Contract

Follow docs/web-ui-plan.md release B and its pinned sibling references. Use only isolated synthetic data. New work remains draft pending promotion.

## Acceptance criteria

- [x] Viewer can search literal text with supported filters and bounded paginated results; missing timestamps have documented date-filter behavior.
- [x] Search shows lag/failure coverage and does not present stale, failed or purged content as current.
- [x] Result links identify generation/event and handle changed sessions with reload guidance.
- [x] Malicious snippets render as plain text; invalid queries/dates/cursors fail safely and device-only requests cannot access search.

## Definition of done

- [x] Focused tests and relevant API/operator docs are complete; record evidence and decisions in the ticket.

## Implementation plan

GET /api/web/v1/search and a GET /search page, both over recall.Index.Search. Parameters: q (3 to 1024 bytes, literal), harness, project, unlinked, since and until (RFC 3339 or YYYY-MM-DD, where a date-only until covers the day), limit (1 to 200, default 50) and cursor. Empty filter values mean no filter, so the plain form submits every field. An empty query shows the form and is never a corpus dump; the API refuses it. Hits carry the native ID and project label from the catalog, so MCP gets them too. The page marks the match from Go-computed byte offsets without building HTML, links each hit to its generation-pinned event, and states index coverage. A lake with no index answers 503 search_unavailable.

## Notes

**agent:claude-code/cd41c9ac** at 2026-09-26T21:02:57Z

### Decisions

- **Date-only until is inclusive of that day.** A date input reads as "through". RFC 3339 values keep the exact exclusive bound. since must be before until.
- **Filters without text are refused (400) on the page as well.** The ticket says an empty query is not a corpus dump. Structured-only queries come in TKT-01M3FPWC9E, which will relax this deliberately.
- **Hits within one session appear newest-indexed first, which within a session means descending position.** Grouping by session was left for later. The screenshot showed it reads fine.

### Acceptance criterion 5 left open
"Explicit session selection is retained for export" is not done. Export (TKT-01M3F2PGR) is still draft, so there is no export screen to carry a selection to. The search results show no selection checkboxes, and nothing implies that every hit downloads. When export is promoted, add selection there and tick this box then.

### Evidence
- Web tests TestSearchAPIIsGuardedValidatedAndCurrent, TestSearchPageMarksMatchesAndEscapes and TestSearchOffWithoutIndex pass. They cover the guard, the device-token refusal, validation, date filters, a stale generation hidden before the index pass, escaping, deep links, paging and no index.
- The browser smoke passes with new search steps: a literal `remote rejected <refs` query is marked, a hit opens with the target event focused, the harness filter works, the invalid-query page shows, and no-JS search paging works.
- `just ci` passes; race runs of web and recall pass.

**agent:claude-code/cd41c9ac** at 2026-09-26T21:03:10Z

Supersedes the 'Acceptance criterion 5 left open' part of the previous note. The criterion (export selection) moved to TKT-01M3F2PGR (Export: add bounded authorized web downloads and shared projection), which owns the export screen it depends on.

## Summary

Landed on branch t3code/explore-store-ui-search in commit b478159. /api/web/v1/search and /search run literal filtered search over recall.Index with coverage, signed paging, marked snippets and generation-pinned links to events. The export-selection criterion moved to TKT-01M3F2PGR. Not yet merged or deployed.
