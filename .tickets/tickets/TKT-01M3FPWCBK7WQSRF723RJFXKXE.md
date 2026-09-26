---
schema: 3
id: TKT-01M3FPWCBK7WQSRF723RJFXKXE
title: "Recall: generation-pinned deep links to events"
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/search
assignees: []
milestone: null
parent: TKT-01M3FPP3H592T31Y2M3N347CPB
origin: null
dependencies:
  - TKT-01M3F2PGDY06D7XE12NWQ9EZF4
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

Give every event an address of session UID, generation and event position, rendered as a viewer URL. The viewer opens the page containing that event and anchors to it. A link whose generation is no longer published says so and offers the current session instead of showing different content at that position. Search hits and event pages carry the link, so the MCP adapter can return it unchanged. Decision 7 of the recall epic applies.

## Acceptance criteria

- [ ] An event link opens the viewer at that event with keyboard focus and a visible highlight.
- [ ] A link to a superseded, failed, pending or purged generation reports that state and links the current session view.
- [ ] Search results and event pages include the link; tests cover stale, malformed and out-of-range links.
