---
schema: 3
id: TKT-01M3S9TW9SD9D0710XR2XQZ6V7
title: "Search snippets: keep line breaks, show tool-call input fields"
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

Snippets currently join the lines of a tool result into one run, which makes recorded `grep -n` output unreadable, and they show a tool call's input as escaped JSON. The decisions are in the parent epic, under "Snippets".

- Keep real newlines and show up to about 4 lines around the match. Use monospace for tool results. Do not strip line-number prefixes, because they are recorded content.
- Show a tool call by its best-describing input field (`command`, `file_path`, `pattern`, `query`) next to the tool badge. When the match falls in another field, such as `description`, show that field and its name.
- With several terms (see the multi-term query ticket), highlight every term inside the window, and show a second window, separated by `…`, when the terms are far apart.
- Do not collapse duplicate snippets across sessions.

The snippet is cut in Go, about 120 bytes either side of the first match (`internal/recall/search.go:28-30, 332-353`). TKT-01M3K45MX may move where snippet text is read from, so check it first.

## Acceptance criteria

- [ ] Tool-result snippets keep their newlines, up to about 4 lines
- [ ] A Bash tool call shows as Bash with its command, not escaped JSON
- [ ] Every query term inside the window is highlighted
