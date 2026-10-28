---
schema: 3
id: TKT-01M61PSX6JF6TP9HE9BXEWMMV6
title: "Normalize: project Codex code-mode inner tool calls"
type: task
status: in-progress
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
claim:
  actor: agent:claude-code/049834bc-norm2
  branch: worktree-agent-a1bbed7644257d0df
  worktree: /home/sothr/workspace/git.local.sothr.com/terva-sh/lampi/.claude/worktrees/agent-a1bbed7644257d0df
  commit: 4ef8a9aaaa17285b97138760d3a554619c5caeb2
  session: null
  claimed_at: 2026-10-28T17:00:56Z
  expires_at: null
archive: null
created_at: 2026-10-28T16:51:58Z
updated_at: 2026-10-28T17:08:51Z
created_by:
  id: agent:claude-code/049834bc-spike
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/049834bc-norm2
  name: Claude Code local agent
extensions: {}
---

## Description

Codex in code mode calls every real tool from inside one `custom_tool_call` named `exec`, whose input is a script. The calls the script makes are written as `event_msg` lines of type `item_completed`, with items `CommandExecution` (command, exit_code, duration, aggregated_output, parsed_cmd), `McpToolCall` (server, tool, arguments, result with isError, duration, error), `FileChange` (changes keyed by path), `ImageView`, `Extension` and `CollabAgentToolCall`. `Codex.eventMsg` has no case for `item_completed`, so all of these become `unknown` events.

The effect, measured in docs/session-diagnostics-calibration.md: in code-mode sessions the only normalized tool is `exec`. No MCP server, command, exit code or path reaches `tool.*`, and `exec` results carry no `is_error`. Tool usage statistics, noisy-server detection, error streaks and repeated-read detection are blank for Codex.

Project these items as tool_call and tool_result pairs keyed by the item id, marked in extra as inner calls of the enclosing cell, so statistics can count context cost (the cell output) apart from tool work (the item). Set `tool.name` to `mcp__<server>__<tool>` for McpToolCall and keep the server in extra, set `is_error` from exit_code or isError, and keep the duration. Inner result bytes are not context bytes; the cell output is.

Outside code mode, `beginTool` sets `tool.name` from `invocation.tool` only and drops `invocation.server`, which loses the MCP server too.

## Acceptance criteria

- [x] McpToolCall, CommandExecution and FileChange items become tool_call and tool_result events with name, call id, is_error and duration
- [x] Inner calls are marked so they can be told apart from the exec cell
- [x] Codex MCP calls keep the server in both modes

## Implementation plan

Read the corpus shape first (structure only, no content): every inner item is an `event_msg` `item_completed` line; there is no `item_started`. Item ids of inner calls are `exec-<uuid>`. `Extension` items carry the id of a top-level `call_...` function call (the `sleep` tool), so they are not inner calls and stay `unknown`. Items arrive while their `exec` cell is open in 90% of cases; the rest arrive after the cell output (a command that outlived its cell), and never while two cells are open.

1. `eventMsg` gets a case for `item_completed`. A `CommandExecution`, `McpToolCall` or `FileChange` item becomes a `tool_call` and a `tool_result`, in that order, both with `raw_type = item_completed` and `tool.call_id` = item id. Every other item kind (Reasoning, AgentMessage, UserMessage, SubAgentActivity, Extension, ImageView, ContextCompaction, CollabAgentToolCall) stays one `unknown` event as now.
2. Names: `CommandExecution` is `exec_command`, `FileChange` is `apply_patch` (the names the calibration doc used), `McpToolCall` is `mcp__<server>__<tool>` through a shared `mcpToolName` that keeps letters, digits, `_` and `-` and turns any other byte into `_` (Claude Code's rule, hyphens kept). The server is kept in extra as `mcp_server`.
3. `is_error`: CommandExecution from `exit_code` (non-zero is true), else `status` (`failed` true, `completed` false). McpToolCall from `result.isError`, else a non-empty `error`, else `status`. FileChange from `status`.
4. Call text: the command, the MCP arguments, the FileChange `changes`. Result text: `aggregated_output`, the MCP result content text or the error, FileChange stdout/stderr. Item fields not used as text stay in extra, split between call (cwd, parsed_cmd, process_id, source, readOnlyHint) and result (exit_code, status, duration, stdout/stderr...). `duration_ms` is added to the result from `{secs, nanos}`. The call takes `started_at_ms` as its recorded time when present.
5. Inner marker: `raw_type = item_completed` on a tool event is the contract; the enclosing cell is `extra.cell_call_id`, the call id of the latest `custom_tool_call` named `exec` (open, or the most recently closed one for a command that outlived its cell). Codex state keeps one string for this.
6. Outside code mode: `beginTool` sets `tool.name` to `mcp__<server>__<tool>` when `invocation.server` is set and keeps `mcp_server` in extra; `endTool` reads `result.Err` / `result.Ok.isError` for MCP results when there is no exit code.
7. Docs: extend "Tool names and errors on results" in docs/harnesses.md with the inner-call contract and re-normalize note; benchmark `BenchmarkCodexStreamInner` on a 32 MiB synthetic code-mode transcript before and after.

Positions: one `item_completed` tool line now yields two events, so later positions in a code-mode session shift by one per inner call. Event ids are fresh ULIDs on every projection anyway. Operators re-normalize with `serve normalize --all`.

## Notes

**agent:claude-code/049834bc** at 2026-10-28T16:54:05Z

Promoted into the session-diagnostics data work by the coordinating agent: without it the tool usage statistics (TKT-01M61MZN8) are blank for this harness. Lands with the tool statistics PR, not the first data PR, so the first PR is not held up.

**agent:claude-code/049834bc-norm2** at 2026-10-28T17:08:50Z

Contract for TKT-01M61MZN8 (tool usage statistics): a Codex code-mode inner call is a `tool_call` or `tool_result` event with `harness = codex` and `raw_type = item_completed`. No other Codex tool event has that raw_type; the exec cell keeps `custom_tool_call` / `custom_tool_call_output`, and non-tool items (Reasoning, AgentMessage, Extension, ...) are `unknown` events with the same raw_type, so filter on event_type as well. No search-index schema change: raw_type is already an indexed column.

- `tool.call_id` is the item id (`exec-<uuid>`), never the cell's call id.
- `extra.cell_call_id` is the latest `exec` cell's call id when the item completed. 10% of inner commands in the local corpus complete after their cell's output (a command that outlived its cell); they still name that cell, or a later one if one started. No item was ever written while two cells were open.
- `extra.item_type` is `CommandExecution`, `McpToolCall` or `FileChange`; `extra.mcp_server` is the server as recorded; `extra.duration_ms` on the result is the item duration.
- Names: `exec_command`, `apply_patch`, `mcp__<server>__<tool>` (letters, digits, `_`, `-` kept; anything else `_`), which matches the names the calibration doc used for inner calls.
- Context bytes come from the cell output, not the inner results.

Alternatives considered for the marker: `item_completed.<ItemKind>` as raw_type was rejected because the raw_type filter is an exact match, so a statistic would have to list three values (and every future kind) to select inner calls, while the kind is already in `extra.item_type` and the name. A boolean in extra was rejected because extra is not indexed.

Benchmark (BenchmarkCodexStreamInner, 32 MiB synthetic transcript made only of exec cells with one command and one MCP call each, 5 runs x 3): before 208-230 ms/op, 192.7 MB/op, 833k allocs; after 297-311 ms/op, 237.4 MB/op, 1.02M allocs. The cost is one more decode of each inner item (the item object, then its text) and twice the events from those lines. A first version that decoded the MCP result twice ran at 357-398 ms/op. Memory stays per line: the only new state is one string (the cell id), bounded by callNameMaxBytes.

Not projected: ImageView, CollabAgentToolCall and Extension items. Extension carries the id of a top-level `call_...` function call (the `sleep` tool) that already has events; the others are rare (72 and 311 in the local corpus) and outside the ACs.
