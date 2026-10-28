---
schema: 3
id: TKT-01M61PT07YZSB6SM2EGT50NSJV
title: "Normalize: project Cursor CLI tool-call and tool-result parts"
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
  claimed_at: 2026-10-28T17:00:57Z
  expires_at: null
archive: null
created_at: 2026-10-28T16:52:01Z
updated_at: 2026-10-28T17:00:57Z
created_by:
  id: agent:claude-code/049834bc-spike
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/049834bc-norm2
  name: Claude Code local agent
extensions: {}
---

## Description

The Cursor CLI store.db blobs hold AI SDK messages whose `content` is a parts array: `tool-call` parts (`toolCallId`, `toolName`, `args`) on assistant messages and `tool-result` parts (`toolCallId`, `toolName`, `result`) on tool messages. `CursorCLI.projectBlob` treats any non-string `content` as one `unknown` event and does not promote parts, so the Cursor CLI harness emits no tool_call or tool_result at all.

MCP calls go through `CallDynamicTool` with `args.namespace` set to the server and `args.toolName` to the tool; namespace `cursor` is Cursor's own dynamic tools (AwaitShell, TodoWrite, Task). `args` is sometimes a JSON-encoded string rather than an object.

Measured in docs/session-diagnostics-calibration.md: 26,525 tool calls in 12 local stores, none visible as tool events. The blobs were read in rowid order, which produced orphan calls and results in 7 of 8 sessions, so the projection also needs the conversation order, for example from the message chain in meta.

Parts carry no timestamps, so latency stays unavailable.

## Acceptance criteria

- [ ] tool-call and tool-result parts become tool_call and tool_result events with name and call id
- [ ] CallDynamicTool calls carry the MCP server and tool, and cursor-namespace tools are marked built-in
- [ ] Events come out in conversation order, not blob rowid order
- [ ] is_error stays null unless the source records a failure, and docs/harnesses.md says so

## Notes

**agent:claude-code/049834bc** at 2026-10-28T16:54:05Z

Absorbs TKT-01M61P84VTN15WV8C40C76MR2B (Normalize: promote Cursor CLI tool-call and tool-result parts), filed in parallel by the normalizer agent; that one's finding is that no error field exists on tool-result parts, captured as AC 4.

**agent:claude-code/049834bc** at 2026-10-28T16:54:05Z

Promoted into the session-diagnostics data work by the coordinating agent: without it the tool usage statistics (TKT-01M61MZN8) are blank for this harness. Lands with the tool statistics PR, not the first data PR, so the first PR is not held up.
