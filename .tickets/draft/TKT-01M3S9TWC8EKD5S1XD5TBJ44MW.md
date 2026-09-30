---
schema: 3
id: TKT-01M3S9TWC8EKD5S1XD5TBJ44MW
title: "Search form: primary filters, project picker, filter chips"
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/search
assignees: []
milestone: null
parent: TKT-01M3S9TCEE0TKQFGS4DSV15ZAF
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-30T13:59:59Z
updated_at: 2026-09-30T13:59:59Z
created_by:
  id: agent:claude-code/cb0b017c
  name: ""
updated_by:
  id: agent:claude-code/cb0b017c
  name: ""
extensions: {}
---

## Description

Twelve controls have equal weight, and several are free-text exact-match fields. Audit filters are out of scope for search (see the parent epic, "What search is for"). The API keeps every parameter, because TKT-01M3FPP3 decision 1 keeps the web and MCP on the same parameters. This ticket changes only what the form shows.

- Always visible: a wide text box with an inline syntax hint (`words · "exact phrase" · -exclude`), a project picker, a harness picker, and a date range with 7d, 30d, all and custom presets.
- The project picker lists slugs built from the catalog and replaces the free-text "Project ID (exact)" field. It uses the slug rule from the session-identity ticket.
- Behind "More filters": event type, actor, and tool. Tool becomes a text field with suggestions from the tool names the lake has seen.
- Remove raw type, "Unlinked projects only" and "Tool errors only" from the form. A URL that sets them still works and shows them as chips.
- Show every active filter as a removable chip above the results.
- No machine filter.
- Rewrite the `e2e/web-smoke.mjs` tool-error check (lines 96-122) to set `tool_error=true` in the URL.

## Acceptance criteria

- [ ] Only text, project, harness and date are visible by default
- [ ] Raw type, unlinked and tool errors are absent from the form but work from the URL as chips
- [ ] The smoke test passes with the rewritten tool-error check
