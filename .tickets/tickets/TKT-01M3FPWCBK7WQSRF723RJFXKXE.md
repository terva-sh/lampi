---
schema: 3
id: TKT-01M3FPWCBK7WQSRF723RJFXKXE
title: "Recall: generation-pinned deep links to events"
type: task
status: in-progress
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
claim:
  actor: agent:claude-code/cd41c9ac
  branch: t3code/explore-store-ui-search
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-946c2db7
  commit: 03fc57ddefb7ce0590ba118544238b9d5b8b4a95
  session: null
  claimed_at: 2026-09-26T21:03:30Z
  expires_at: null
archive: null
created_at: 2026-09-26T20:35:35Z
updated_at: 2026-09-26T21:03:58Z
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

- [x] An event link opens the viewer at that event with keyboard focus and a visible highlight.
- [x] A link to a superseded, failed, pending or purged generation reports that state and links the current session view.
- [x] Search results and event pages include the link; tests cover stale, malformed and out-of-range links.

## Implementation plan

The event address is session UID + generation + position, rendered by recall.EventLink as /sessions/{uid}/transcript?gen=G&at=N#e-N. The viewer (TKT-01M3F2PGD) opens the page starting five events before the target, marks it, and moves keyboard focus there (lake.js). This ticket completes the unavailable states: superseded generation (409), pending and failed (409, with a link to session details), a position past the end (200, naming the event count), and a purged or unknown session (an HTML 404 page, not the JSON error). Search hits (TKT-01M3F2PGM) and event pages carry the same link field, so MCP can return it unchanged.

## Notes

**agent:claude-code/cd41c9ac** at 2026-09-26T21:03:58Z

Evidence: web TestDeepLinksReportEveryUnavailableState (out of range, in range, pending, failed, purged) and TestTranscriptPageRendersLiterallyAndHandlesStaleLinks (superseded, malformed) pass. The browser smoke covers focus and highlight from a transcript permalink and from a search hit. Decision: a position past the end returns 200 with a message, not 404, because the session and generation exist and the link can still offer the start of the transcript.
