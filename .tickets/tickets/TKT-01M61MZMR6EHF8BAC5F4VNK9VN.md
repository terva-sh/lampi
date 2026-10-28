---
schema: 3
id: TKT-01M61MZMR6EHF8BAC5F4VNK9VN
title: "Normalize: name Claude Code tool results from their call"
type: bug
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
  actor: agent:claude-code/049834bc-norm
  branch: worktree-agent-ae1d2160e416e8e66
  worktree: /home/sothr/workspace/git.local.sothr.com/terva-sh/lampi/.claude/worktrees/agent-ae1d2160e416e8e66
  commit: 2439f8a0f31b8907b1905d5ac3ea5bd602ed46da
  session: null
  claimed_at: 2026-10-28T16:30:48Z
  expires_at: null
archive: null
created_at: 2026-10-28T16:20:09Z
updated_at: 2026-10-28T16:35:39Z
created_by:
  id: agent:claude-code/049834bc
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/049834bc-norm
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

## Implementation plan

### Approach

- Add a bounded call-id-to-name table, `callNames`, in internal/normalize/callnames.go. It keeps at most 4096 entries and forgets the oldest first. A call id or a name longer than 256 bytes is not kept, so the table holds at most about 2 MiB on a hostile transcript and a few hundred KiB on a real one. Calls precede their results in every line-oriented format, so a FIFO window of recent calls covers the results that arrive.
- Claude: `claudeState` gets a `calls callNames`. A `tool_use` or `server_tool_use` block records its id and name. `toolResultEvent` and `searchResultEvent` fill `tool.name` from it. A result whose call is not in the table keeps a null name.
- Every other normalizer: Codex `endTool` and `functionOutput`, and terva `tool_result`, set only `CallID` today. Give `codexState` and `metaState` the same table. Codex keeps two tables, one for `response_item` calls and one for `event_msg` begin events, because one call id appears in both with different names (`shell` against `exec_command`), and each result should carry the name of its own call event. Grok falls back to the table when the update has no title, `_meta` tool or kind. Cursor IDE falls back to the bubble's own call when a `toolResults` element has no name. OpenCode and Cursor IDE `toolFormerData.result` already carry the name. Cursor CLI and grokbot emit no tool results at all.
- State is per projection pass. Projection is always a full pass (docs/incremental-projection.md): no checkpoint resumes a parser state, so the table needs no persistence. If a checkpoint protocol lands later, this table is parser state it has to cover.

### Generation change and re-normalize

There is no automatic requeue when a normalizer changes. A session already grows into a full re-projection on its next upload, so active sessions pick up the names on their own. Older sessions need `terva-lampi serve normalize --all`, which `docs/cli.md` already describes as the step after an upgrade that changes how derived files are written. The decision is to say that in the release notes rather than force a requeue on start: a forced requeue would re-project every session on every lake at upgrade, while the operator can choose when to pay that. SchemaVersion stays 1, because no field changes meaning; a null name becomes a value.

### Cost measurement

The internal lake is not readable from this session and must not be touched, so measure `BenchmarkClaudeStreamTools` (a 32 MiB synthetic Claude transcript of tool call and result pairs, distinct ids) before and after, and the internal/api test time before and after.
