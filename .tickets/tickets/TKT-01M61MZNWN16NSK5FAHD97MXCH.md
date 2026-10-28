---
schema: 3
id: TKT-01M61MZNWN16NSK5FAHD97MXCH
title: "Tool usage: estimate starting-context cost of MCP servers"
type: task
status: ready
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
  - TKT-01M61MZMTSK36M0TB5ZZFF66RA
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-28T16:20:10Z
updated_at: 2026-10-28T16:35:44Z
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

## Implementation plan

- Uses the token columns added in TKT-01M61MZMT (input_tokens, cache_read_tokens, cache_write_tokens). Starting context per session = input + cache_read + cache_write of the first usage row by position.
- For each origin, compare the median starting context of in-scope sessions that use it with those that do not, within the same harness. Report sessions on each side and the difference. Suppress a comparison with fewer than M sessions on either side.
- Exposed as an optional `context` block in the tool stats response (`--context` on the CLI, a column on the Tools page), labelled as a correlation.
- Check whether Codex rollouts record the tool list (the spike looks). If they do, report a server's defined-tool count directly for that harness.
