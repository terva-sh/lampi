---
schema: 3
id: TKT-01M3FPWC9E15XG1GFS7886Z415
title: "Recall: structured event filters shared by web search and MCP"
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/search
  - area/catalog
assignees: []
milestone: null
parent: TKT-01M3FPP3H592T31Y2M3N347CPB
origin: null
dependencies:
  - TKT-01M3F2PGHM6VHQBNE5XKDXS407
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-26T20:35:35Z
updated_at: 2026-09-26T21:09:05Z
created_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
extensions: {}
---

## Description

Add filters on event_type, actor, tool name, tool error and raw_type to the shared recall query layer. They combine with literal text, the session filters (harness, project, unlinked) and the UTC recorded-time range, and they also work with no text at all, for example every failed tool call in one project. Filters are exact-match on values from a bounded vocabulary or length-capped strings; no user SQL. The web search page and API expose them through the same parameters the MCP adapter will use. Decisions 1 and 6 of the recall epic apply.

## Acceptance criteria

- [x] Structured filters work alone and combined with text, session filters and time range, with bounded deterministic paging.
- [x] Invalid filter values fail with the documented validation error; values are bound parameters, never SQL.
- [x] Web search page and JSON API expose the filters; tests cover each filter and combinations on synthetic data.

## Implementation plan

Extend recall.SearchRequest with exact event filters: event_type and actor from the schema vocabulary, plus unreadable; tool name and raw_type as exact strings of at most 256 bytes; tool_error as an exact flag. They combine with text, session filters and the time range. Without text, a request needs at least one event filter, so session filters alone cannot list the corpus. Structured-only queries walk docs.id newest first through new indexes docs_type, docs_tool and docs_error. Text queries keep walking the FTS rowid; the order and keyset column follow the mode, so neither sorts in a temp b-tree. The web API and the form expose the same parameter names the MCP adapter will use.

## Notes

**agent:claude-code/cd41c9ac** at 2026-09-26T21:09:05Z

### Decisions
- **tool_error matches the recorded flag exactly.** A tool result whose harness did not record success or failure (null) matches neither true nor false. Treating null as false would claim the harness said "no error". Documented in web-api.md.
- **The order and keyset column depend on the mode.** The first version ordered by d.id for both. EXPLAIN on the real query showed `USE TEMP B-TREE FOR ORDER BY` for text searches, which sorts every match. searchSQL now uses fts.rowid with text and d.id without, and queryPlan() in the tests explains the real SQL for each mode.
- **No index for actor-only queries.** Actor has five values, so an index would not narrow much. Actor-only walks the primary key newest first until the page fills, within the 5 s read budget.
- **Index version stays 1 despite the new indexes.** search.db has not shipped (the branch is unmerged). A version bump would only rebuild dev files.

### Evidence
Plans:
- text: `SCAN fts VIRTUAL TABLE INDEX 192:M1; SEARCH d USING INTEGER PRIMARY KEY`
- event_type: `SEARCH d USING INDEX docs_type`
- tool: `SEARCH d USING INDEX docs_tool`
- tool_error: `SEARCH d USING INDEX docs_error`

No mode sorts in a temp b-tree. recall TestStructuredFilters and web TestStructuredSearchThroughAPIAndPage pass. The browser smoke's "structured search without text" step (every failed Bash result, each with the tool error badge) passes. `just ci` passes.

## Summary

Landed in 0bab144 on t3code/explore-store-ui-search. Search takes event_type, actor, tool, tool_error and raw_type, with or without text, through recall.SearchRequest, /api/web/v1/search and the /search form. Not yet merged or deployed.
