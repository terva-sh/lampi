---
schema: 3
id: TKT-01M61MZMR6EHF8BAC5F4VNK9VN
title: "Normalize: name Claude Code tool results from their call"
type: bug
status: done
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
updated_at: 2026-10-28T16:42:32Z
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

- [x] A Claude Code tool_result whose call_id matches an earlier tool_call in the session carries that call's tool.name.
- [x] A result with no matching call keeps a null name; the call map is bounded per session.
- [x] Every other normalizer is checked for names on results, and the finding is recorded in a note.
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

## Notes

**agent:claude-code/049834bc-norm** at 2026-10-28T16:42:05Z

### Names on results, per normalizer (checked 2026-10-28)

- claude: null before. Now filled from the earlier `tool_use` or `server_tool_use` with the same id, for `tool_result` and `web_search_tool_result`.
- codex: null before on every result (`*_end` event_msg and `function_call_output`, `custom_tool_call_output`, `tool_search_output`). Now filled from the same-kind call: `response_item` outputs from `function_call`/`custom_tool_call`/`tool_search_call`, and `event_msg` `*_end` from `*_begin`. Two tables, because one call id is `shell` as a function_call and `exec_command` as an exec_command_begin. `web_search_call` has no separate result event.
- terva: null before on `tool_result` blocks. Now filled from the `tool_call` block.
- grok: name came from the update's own `title`, `_meta` `x.ai/tool` or `kind`. An ACP `tool_call_update` usually carries none of them, so the existing fixture's results were null. Now falls back to the `tool_call`'s name. Note that Grok's "name" is the ACP title (for example "Read"), not a stable tool id.
- opencode: already named (`tool` on the same part; call and result come from one part).
- cursor (IDE): already named for `toolFormerData.result`. A `toolResults` element with no `name` was null; it now takes the bubble's call name when the call id matches. An element of another call id stays null.
- cursor-cli: emits no tool results at all. The projector keeps content parts on one `unknown` event (by design, cursorcli.go header). Local stores show `tool-call` and `tool-result` parts with `toolCallId` and `toolName`, so promoting them is possible as separate work.
- grokbot: emits no tool results. `tool` role lines are skipped; they carry only text parts.

ShareGPT/trajectory export: tool turns now carry `name` where the result has one (sharegpt_test.go expectation updated).

**agent:claude-code/049834bc-norm** at 2026-10-28T16:42:05Z

### Generation change, re-normalize and cost

Decision: no forced requeue and no SchemaVersion bump. No field changes meaning; a null `tool.name` on a result becomes a value. Every projection is a full pass (docs/incremental-projection.md), so a session that grows is re-projected with names on its next upload. Older sessions are filled by `terva-lampi serve normalize --all`, the existing post-upgrade step in docs/cli.md. docs/harnesses.md ("Tool names and errors on results") now says this. The next release's notes must carry the same line; no release-notes file exists yet for the release after v0.9.0, so none was created here.

Alternatives: a forced requeue at lake start re-projects every session on every lake at upgrade, whether or not anyone will query tool names, and competes with live ingest; rejected in favour of the operator choosing when. Lazy-only (growth) leaves finished sessions null forever; acceptable as the default but the `--all` step is documented for operators who want the statistics over history.

Cost on the internal lake: not measured. That lake is not readable from this session and must not be touched. Measured instead:

- `BenchmarkClaudeStreamTools` (new, internal/normalize/claude_test.go): a 32 MiB synthetic Claude transcript of ~6,900 tool_use/tool_result pairs with distinct ids, so the table fills and evicts. Six runs each, `-benchtime 5x`, Ryzen 9 5950X, GOMAXPROCS 12. Before (2439f8a): median 312 ms/op (305-321), 244.9 MB/op, 925.8k allocs/op. After: median 311 ms/op (303-320), 245.9 MB/op, 932.6k allocs/op. CPU is within noise; allocation +0.4%, one map entry per call.
- internal/api tests: 7.8 s before, 8.1 s after alone, 9.7 s inside the full `just ci` run (parallel load). No measurable growth from the change.
- The cost of `serve normalize --all` on a lake is therefore the existing full re-projection cost per session, essentially unchanged by this ticket; see docs/performance.md for that per-MiB cost (about 1.1 s stage wall for 32 MiB).

The AC asks for the cost on the internal lake, so it stays unticked.

## Summary

A Claude Code `tool_result` and `web_search_tool_result` now carries `tool.name` from the earlier `tool_use` with the same id, through a bounded per-pass table (internal/normalize/callnames.go: the 4096 most recent calls; ids and names over 256 bytes are not kept). A result with no call in the table keeps a null name. The same join now names Codex results (one table per call kind), terva results, and Grok updates that carry no title, tool or kind. A Cursor IDE `toolResults` element of the bubble's own call takes the call's name. OpenCode was already named. Cursor CLI and grokbot emit no tool results; follow-up TKT-01M61P84VTN15WV8C40C76MR2B (Normalize: promote Cursor CLI tool-call and tool-result parts) covers Cursor CLI.

Re-normalize: no forced requeue and no schema bump. Growing sessions are re-projected on their next upload; operators run `terva-lampi serve normalize --all` to fill the rest, as docs/harnesses.md now says. The next release's notes must repeat that.

Cost: the 32 MiB synthetic tool-pair benchmark is unchanged within noise (median 312 ms/op before, 311 after; +0.4% allocations); internal/api tests 7.8 s before and 8.1 s after. The internal lake's cost was not measured because that lake was out of reach, so AC 4 stays unticked; see the note.

Tests: TestClaudeToolResultNameFromCall (slice and stream paths), TestCallNamesBound, TestCodexResultNamesFollowTheirOwnCall, plus name assertions in the Codex, terva, Grok and ShareGPT tests. `GOFLAGS=-mod=mod just ci` passes.
