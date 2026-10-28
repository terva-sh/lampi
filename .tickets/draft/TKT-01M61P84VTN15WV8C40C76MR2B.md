---
schema: 3
id: TKT-01M61P84VTN15WV8C40C76MR2B
title: "Normalize: promote Cursor CLI tool-call and tool-result parts"
type: task
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
created_at: 2026-10-28T16:42:16Z
updated_at: 2026-10-28T16:42:16Z
created_by:
  id: agent:claude-code/049834bc-norm
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/049834bc-norm
  name: Claude Code local agent
extensions: {}
---

## Description

The Cursor CLI projector (internal/normalize/cursorcli.go) keeps a blob whose `content` is a parts array as one `unknown` event, so a Cursor CLI session has no `tool_call` or `tool_result` events, and tool statistics are blank for that harness.

Local `store.db` files (read for key names only, under TKT-01M61MZMXHF57GKYYRE9ZQ4S3S) show the parts are AI SDK shaped: an assistant blob has `tool-call` parts with `toolCallId`, `toolName` and `args`; a `tool` role blob has `tool-result` parts with `toolCallId`, `toolName`, a `result` string and `experimental_content`. No error field was seen, so `is_error` would stay null.

Promote those parts into `tool_call` and `tool_result` events with `tool.name` and `tool.call_id`, keep text and reasoning parts as they are today or promote them on the same rule, and bump the Cursor CLI reader version if the adapter pin requires it.

## Acceptance criteria

- [ ] A Cursor CLI tool-call part becomes a tool_call event with tool.name and tool.call_id.
- [ ] A Cursor CLI tool-result part becomes a tool_result event with tool.name and tool.call_id, and is_error stays null unless the source records a failure.
