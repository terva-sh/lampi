---
schema: 3
id: TKT-01M61NTM0K2TQHX28T2R4HN0TN
title: "Error trends: error events and failed tool results over time"
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/search
  - area/server
assignees: []
milestone: null
parent: TKT-01M61NT6XQ5WA0XZ0AXZEYMHHG
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-28T16:34:53Z
updated_at: 2026-10-28T16:38:41Z
created_by:
  id: agent:claude-code/d4b24133
  name: ""
updated_by:
  id: agent:claude-code/d4b24133
  name: ""
extensions: {}
---

## Description

Show error trends from data the lake already indexes.

- Count `error` events and tool results with `is_error` true, by hour or day, with a harness filter. Both kinds are in the search index (`event_type`, `tool_error`, `recorded_ns`).
- List the sessions with the most errors in the range, linking to the session page.
- Join a tool result to its call by `call_id` within the session to name the failing tool, because Claude and Codex results carry no tool name.
- Unreadable-line errors written by the normalizer are lake problems, not session errors; count them apart or leave them out.

## Acceptance criteria

- [ ] Errors by hour or day with a harness filter
- [ ] Failed tool results are named by joining on call_id
