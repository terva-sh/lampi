---
schema: 3
id: TKT-01M61MZMR6EHF8BAC5F4VNK9VN
title: "Normalize: name Claude Code tool results from their call"
type: bug
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/normalize
assignees: []
milestone: null
parent: TKT-01M61MYZ6H6J8MGNPWHS8MTJ1X
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-28T16:20:09Z
updated_at: 2026-10-28T16:20:09Z
created_by:
  id: agent:claude-code/049834bc
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/049834bc
  name: Claude Code local agent
extensions: {}
---

## Description

Claude Code writes a tool result as a `tool_result` block that names only `tool_use_id`. `toolResultEvent` and `searchResultEvent` (internal/normalize/claude.go:293-327) copy that into `tool.call_id` and leave `tool.name` null. The matching `tool_call` event earlier in the session carries the name.

Because the result is where the large output lives, grouping result sizes or errors by tool, or by MCP server, is impossible for Claude Code without a join, and `--count-by tool.name` over results counts them under null.

Fill `tool.name` on a result from the call with the same `call_id` in the same session. The normalizer state (`claudeState`) can keep a call id to name map, bounded per session. A result with no matching call keeps a null name.

This changes stored output, so it needs a normalization generation change and a re-normalize of existing Claude Code sessions. Decide whether that runs as a forced requeue or lazily on the next growth, and record the cost on the internal lake.

Also check every other normalizer: Codex, OpenCode, Grok, grokbot, terva, Cursor IDE and Cursor CLI appear to set `Name` on results (for example cursor.go:308), but this was not verified for each.

## Acceptance criteria

- [ ] A Claude Code tool_result whose call_id matches an earlier tool_call in the session carries that call's tool.name.
- [ ] A result with no matching call keeps a null name; the call map is bounded per session.
- [ ] Every other normalizer is checked for names on results, and the finding is recorded in a note.
- [ ] The generation change and re-normalize plan are decided and their cost on the internal lake is recorded.
