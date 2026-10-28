---
schema: 3
id: TKT-01M61MZNWN16NSK5FAHD97MXCH
title: "Tool usage: estimate starting-context cost of MCP servers"
type: task
status: draft
status_reason: null
priority: low
due_on: null
labels:
  - area/search
assignees: []
milestone: null
parent: TKT-01M61MYZ6H6J8MGNPWHS8MTJ1X
origin: null
dependencies:
  - TKT-01M61MZN3QPKF04MAN3TWE5K0F
  - TKT-01M61MZN8YEFZ90AZDW62QG2DZ
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-28T16:20:10Z
updated_at: 2026-10-28T16:20:10Z
created_by:
  id: agent:claude-code/049834bc
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/049834bc
  name: Claude Code local agent
extensions: {}
---

## Description

A server's tool definitions take context at session start even when none of its tools is called, and transcripts generally do not record them. Estimate that cost from the first `usage` event's input plus cache tokens (the starting context), compared across sessions grouped by which servers appear.

This only shows a correlation and cannot see a server that is configured but never called, so present it as a hint beside the tool usage statistics, not as a finding. Check whether any harness records its tool list (Codex rollouts may) and use that where present.

## Acceptance criteria

- [ ] Starting context tokens are compared across sessions grouped by the servers they use.
- [ ] The result is labelled as a correlation hint, not a finding.
- [ ] Any harness that records its tool list is used directly.
