---
schema: 3
id: TKT-01M61PSX6JF6TP9HE9BXEWMMV6
title: "Normalize: project Codex code-mode inner tool calls"
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
created_at: 2026-10-28T16:51:58Z
updated_at: 2026-10-28T16:51:58Z
created_by:
  id: agent:claude-code/049834bc-spike
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/049834bc-spike
  name: Claude Code local agent
extensions: {}
---

## Description

Codex in code mode calls every real tool from inside one `custom_tool_call` named `exec`, whose input is a script. The calls the script makes are written as `event_msg` lines of type `item_completed`, with items `CommandExecution` (command, exit_code, duration, aggregated_output, parsed_cmd), `McpToolCall` (server, tool, arguments, result with isError, duration, error), `FileChange` (changes keyed by path), `ImageView`, `Extension` and `CollabAgentToolCall`. `Codex.eventMsg` has no case for `item_completed`, so all of these become `unknown` events.

The effect, measured in docs/session-diagnostics-calibration.md: in code-mode sessions the only normalized tool is `exec`. No MCP server, command, exit code or path reaches `tool.*`, and `exec` results carry no `is_error`. Tool usage statistics, noisy-server detection, error streaks and repeated-read detection are blank for Codex.

Project these items as tool_call and tool_result pairs keyed by the item id, marked in extra as inner calls of the enclosing cell, so statistics can count context cost (the cell output) apart from tool work (the item). Set `tool.name` to `mcp__<server>__<tool>` for McpToolCall and keep the server in extra, set `is_error` from exit_code or isError, and keep the duration. Inner result bytes are not context bytes; the cell output is.

Outside code mode, `beginTool` sets `tool.name` from `invocation.tool` only and drops `invocation.server`, which loses the MCP server too.

## Acceptance criteria

- [ ] McpToolCall, CommandExecution and FileChange items become tool_call and tool_result events with name, call id, is_error and duration
- [ ] Inner calls are marked so they can be told apart from the exec cell
- [ ] Codex MCP calls keep the server in both modes
