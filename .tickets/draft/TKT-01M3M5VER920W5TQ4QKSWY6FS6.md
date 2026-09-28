---
schema: 3
id: TKT-01M3M5VER920W5TQ4QKSWY6FS6
title: Search marks every tool result as a tool error
type: bug
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/search
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-28T14:14:11Z
updated_at: 2026-09-28T14:14:21Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/e4a47e8c
  name: ""
extensions: {}
---

## Description

Every `tool_result` on the Search page shows a "Tool Error" badge,
including results that succeeded. `internal/web/templates/page.html`
renders it with `{{with .ToolError}}{{if .}}`. `ToolError` is a
`*bool`, and a template's `if` on a pointer is true for any non-nil
pointer, so `false` shows as an error too. It should compare the value
the pointer holds.
