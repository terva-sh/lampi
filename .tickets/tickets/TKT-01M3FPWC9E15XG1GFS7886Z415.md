---
schema: 3
id: TKT-01M3FPWC9E15XG1GFS7886Z415
title: "Recall: structured event filters shared by web search and MCP"
type: task
status: ready
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
updated_at: 2026-09-26T20:35:43Z
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

- [ ] Structured filters work alone and combined with text, session filters and time range, with bounded deterministic paging.
- [ ] Invalid filter values fail with the documented validation error; values are bound parameters, never SQL.
- [ ] Web search page and JSON API expose the filters; tests cover each filter and combinations on synthetic data.
