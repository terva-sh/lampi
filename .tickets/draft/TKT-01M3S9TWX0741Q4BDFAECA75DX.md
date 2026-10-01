---
schema: 3
id: TKT-01M3S9TWX0741Q4BDFAECA75DX
title: "Search: copy an event from its hit row"
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
created_at: 2026-09-30T14:00:00Z
updated_at: 2026-09-30T14:00:00Z
created_by:
  id: agent:claude-code/cb0b017c
  name: ""
updated_by:
  id: agent:claude-code/cb0b017c
  name: ""
extensions: {}
---

## Description

Finding how something was used before usually ends with copying a command or output. Copying exists only on the transcript page today. The decisions are in the parent epic, under "Hit rows".

- Each hit gets a quiet copy action.
- With JavaScript on, it copies the event's full text to the clipboard.
- With JavaScript off, it links to the plain-text excerpt for that event, `/sessions/{uid}/excerpt?gen=G&from=P&count=1` (`excerptURL` at `internal/web/pages.go:72`), pinned to the generation like the deep link.

## Acceptance criteria

- [ ] Copy puts the event text on the clipboard with JS on
- [ ] With JS off the copy link opens the generation-pinned plain-text excerpt
