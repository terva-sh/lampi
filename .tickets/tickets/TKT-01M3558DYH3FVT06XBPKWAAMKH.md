---
schema: 3
id: TKT-01M3558DYH3FVT06XBPKWAAMKH
title: Optional terva hook nudge (shipped example)
type: task
status: ready
status_reason: null
priority: low
due_on: null
labels:
  - area/agent
  - phase/2-harness
assignees: []
milestone: phase-2
parent: TKT-01M3558DV5NHYZBFMPV1AVRNYQ
origin: null
dependencies:
  - TKT-01M3558DTHC06337Q51V6ERPTD
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-22T18:15:12Z
updated_at: 2026-09-23T12:27:00Z
created_by:
  id: human:sothr
  name: Drew Short
updated_by:
  id: agent:cursor/e4d5
  name: Cursor cloud agent
extensions: {}
---

## Description

Polish and document the enqueue hook as a supported optional acceleration.

## Acceptance criteria

- [ ] The enqueue hook is polished and documented as a supported optional acceleration
- [ ] The filesystem watch remains the source of truth

## Notes

**agent:cursor/e4d5** at 2026-09-23T12:27:00Z

The example hook landed in #14 as hooks/terva-post-tool-enqueue.sh, under TKT-01M3558DTHC06337Q51V6ERPTD (Example terva post_tool_use enqueue hook). deploy/README.md describes it as an uninstalled example: it signals agent.pid with SIGUSR1 when that process is terva-lampi, and the directory watch stays the source of truth.

That example is not this ticket. The description asks to polish the hook and document it as a supported optional acceleration. Those two are still open, so this stays ready.
