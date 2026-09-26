---
schema: 3
id: TKT-01M3FPWCDFXXCHD8F5PA0GGMWP
title: "Recall: copy a selected event span out in paste-ready form"
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

Let a viewer select a contiguous span of events in one session and copy it as plain text ready to paste into a new agent session, with a header naming the session, harness, project and deep link. The same span rendering is a shared recall function so the MCP adapter can return it. Span length and output bytes are bounded. Decision 8 of the recall epic applies. Authorization decision: copy-out is a viewer action because it returns only text the viewer role can already read on screen; bulk downloads and training formats stay behind release B's exporter role and export_projects policy.

## Acceptance criteria

- [ ] A viewer can select a bounded span in the transcript and copy it; without JavaScript the same text is available as a plain-text page.
- [ ] Rendering is shared, bounded by events and bytes, keeps order, marks truncation and never decrypts opaque content.
- [ ] Tests cover span validation, bounds, stale generations and malicious content.
