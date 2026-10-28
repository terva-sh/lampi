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
updated_at: 2026-10-28T17:30:55Z
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

- [x] tool-call and tool-result parts become tool_call and tool_result events with name and call id
- [x] CallDynamicTool calls carry the MCP server and tool, and cursor-namespace tools are marked built-in
- [x] Events come out in conversation order, not blob rowid order
- [x] is_error stays null unless the source records a failure, and docs/harnesses.md says so

## Implementation plan

Measured the local stores (structure only, read through a read-only snapshot): the export lists blobs by id (`ORDER BY id`), and ids are content hashes, so export order is random with respect to the conversation. Protobuf root blobs (base64 wrappers in the export) hold field 1 once per message blob id (32 raw bytes) in conversation order; `latestRootBlobId` on meta key "0" names the newest root. A root exists per conversation change (11,746 in the largest store), each extending the previous one except across compactions, where a new list restarts from the system message. Every JSON blob is listed by some root.

1. `cursorCLIOrder` (new file `cursorcliorder.go`): decode every root's field-1 ids, add an edge between consecutive ids of each list (deduplicated), and emit a reverse-postorder depth-first walk: a topological order, so each list's order holds and a call precedes its result. Successors and walk starts are visited newest first (latest-root members by position, then export index), which puts the latest root's branch last after a compaction. Cycles are broken where met. Unlisted blobs (roots included) follow in export order; no roots means export order. Ids are keyed as `[32]byte` so lookups do not allocate.
2. `projectBlob` returns events: a parts blob stays one `unknown` event with the tool parts removed from `extra.content`, followed by one `tool_call` (raw_type `tool-call`) or `tool_result` (raw_type `tool-result`) per tool part, in part order, with `extra.blob_id`.
3. Names: `CallDynamicTool` reads `args` (object or JSON string): namespace `cursor` gives the bare `args.toolName`, any other namespace gives `mcp__<namespace>__<toolName>` via the shared `mcpToolName` with `extra.mcp_server`; `extra.cursor_tool_name` keeps `CallDynamicTool`. `extra.builtin` is true on every non-MCP call. Results take the call's name from a bounded `callNames` table, else the part's own toolName.
4. `is_error` stays null (no source field); `recorded_at` stays ingest time (no part timestamps).
5. Docs: "Cursor CLI tool parts" under "Tool names and errors on results" in docs/harnesses.md, re-normalize note extended. Benchmark `BenchmarkCursorCLITools` before/after, plus a run over the local stores under the 256 MiB export cap to count orphans.

## Notes

**agent:claude-code/049834bc** at 2026-10-28T16:54:05Z

Absorbs TKT-01M61P84VTN15WV8C40C76MR2B (Normalize: promote Cursor CLI tool-call and tool-result parts), filed in parallel by the normalizer agent; that one's finding is that no error field exists on tool-result parts, captured as AC 4.

**agent:claude-code/049834bc** at 2026-10-28T16:54:05Z

Promoted into the session-diagnostics data work by the coordinating agent: without it the tool usage statistics (TKT-01M61MZN8) are blank for this harness. Lands with the tool statistics PR, not the first data PR, so the first PR is not held up.

**agent:claude-code/049834bc-norm2** at 2026-10-28T17:30:50Z

Findings and measurements.

The ticket's "rowid order" is in practice id order: the adapter exports `SELECT id, data FROM blobs ORDER BY id`, and ids are content hashes. The normalizer never sees rowid, so the order has to come from the roots. Roots form one list per conversation state; the walk merges them (see plan). `latestRootBlobId` alone is not enough: in the largest store it lists 156 of 18,271 message blobs, because compaction drops older messages from the current context.

Real-store run (local, read-only snapshot through the adapter's own export, stores under the 256 MiB export cap, 9 stores with tables): every tool-call and tool-result part became an event, and no result came before its call in any session (0 orphan results; the spike measured orphans in 7 of 8 sessions in rowid order). One store had 366 results for 355 calls: Cursor rewrites a message blob when it changes, and an older copy repeats a result part. Those duplicates are emitted as they are; deduplicating them by call id would hide the blob history and was left out.

Timing on the largest exportable store (270 MiB export, 2,576 calls): old projection 2.33 s, new 3.16 s, under machine load average 8 to 12. Benchmark `BenchmarkCursorCLITools` (600 turns, a root per turn, 19 MB export), back to back: old 137-141 ms/op, 124 MB/op, 145k allocs; new 207-216 ms/op before keying ids as [32]byte (541k allocs), then 228-275 ms/op, 148 MB/op, 176k allocs under a busier machine. The added cost is decoding every root's base64 once, which is most of a store's bytes, and the tool-part split. Memory stays the whole-document model the projector already has: the order adds an id index, an edge set and one list buffer, all proportional to the number of blobs and distinct edges, not to root bytes.

Not done: text and reasoning parts are still not promoted to message events (outside this ticket), and `mcpDetails` on CallDynamicTool args is left in the call text, not parsed.
