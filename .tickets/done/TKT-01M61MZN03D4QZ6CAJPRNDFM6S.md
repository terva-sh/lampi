---
schema: 3
id: TKT-01M61MZN03D4QZ6CAJPRNDFM6S
title: "Spike: calibrate tool size and session smell thresholds"
type: spike
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
  - question
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
updated_at: 2026-10-28T16:52:31Z
created_by:
  id: agent:claude-code/049834bc
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/049834bc-spike
  name: Claude Code local agent
extensions: {}
---

## Description

Before building the diagnosis engine or fixing thresholds, measure on the internal lake how often each candidate signal fires and what real tool naming looks like.

Work offline on a copy, without touching the live lake or agent (docs/development.md, "Develop on a machine that runs lampi"). DuckDB reads the normalized `.jsonl.zst` events files directly (see the comment at the top of internal/normalize/eventsfile.go).

Questions to answer:

- Distribution of tool result and call sizes, per tool and per MCP server (join Claude Code results to calls on `call_id` until results carry their tool name).
- How often each candidate smell fires per session at a few thresholds: large result, large input, error streak, retry loop on the same input hash, repeated read of one path, compactions, input token growth, orphaned call or result.
- How each harness names MCP and extension tools. Claude Code uses `mcp__<server>__<tool>`, and plugin servers seem to add a `plugin_` part; Codex, OpenCode, Cursor and terva are unchecked.
- Whether timestamps on calls and results are good enough for latency in each harness.

Record the numbers and the chosen thresholds in a doc (or a section of docs/performance.md) with the queries used. Report counts and sizes only, no content.

## Acceptance criteria

- [ ] Size distributions per tool and MCP server on the internal lake are recorded with the queries used.
- [x] Firing rates for each candidate smell at several thresholds are recorded, and thresholds are chosen.
- [ ] MCP and extension tool naming is recorded for each harness.
- [ ] Latency feasibility per harness is recorded.

## Implementation plan

1. Write a throwaway Go program in an uncommitted directory inside the module (internal/zz_spike) that walks each local transcript store read-only, streams Claude Code and Codex files through normalize.Claude/normalize.Codex Stream, and reads Cursor CLI parts from scratch copies of store.db.
2. Per session, keep only bounded state: call_id -> (tool, input size, input sha256, timestamp) to join results to calls, per-tool byte sums, recent-call window for retry detection, per-path read counters, usage series summary. Emit one aggregate record per session and per tool call (sizes and names only) into scratch.
3. Aggregate offline: size percentiles per tool, per origin (built-in / MCP / other) and per harness; top-tool and top-MCP-server share of result bytes; firing rates for each smell at 2-3 thresholds; MCP and plugin name patterns; timestamp coverage and latency percentiles.
4. Write docs/session-diagnostics-calibration.md with aggregates, methodology and recommended defaults per finding code; no names beyond built-in tools and public products.
5. Delete the throwaway program; leave only the doc and ticket changes committed.

## Notes

**agent:claude-code/049834bc-spike** at 2026-10-28T16:34:09Z

Data source substitution: the live internal lake (/var/lib/terva-lampi) is not readable from this session and must not be touched, and no read token is available. The spike measures the raw transcripts on this workstation instead, which are what the lake ingests: the Claude Code projects store, the Codex sessions store, the Cursor CLI store.db files (chats and acp-sessions), and the OpenCode database. Claude Code and Codex files are streamed through lampi's own normalizers (normalize.Claude / normalize.Codex Stream) so the numbers reflect normalized events. Every source is opened read-only; SQLite stores are copied into a scratch directory first and only the copy is opened. The Cursor CLI normalizer projects a blob whose content is a parts array as one `unknown` event, so it emits no tool_call or tool_result; Cursor CLI numbers are therefore taken from the AI-SDK parts (`tool-call` / `tool-result`) in the copied blobs, and are marked as raw rather than normalized. The OpenCode database on this machine is the session_v2/session_message schema, not the `opencode export` document the normalizer reads, and holds 8 sessions with 3 tool calls, so OpenCode is described structurally only. No Cursor IDE state.vscdb, Grok or terva store exists on this machine.

**agent:claude-code/049834bc-spike** at 2026-10-28T16:52:21Z

Results are in docs/session-diagnostics-calibration.md (commit 686bbc3). Corpus: 1,004 Claude Code files (110 main, 894 subagent), 88 Codex files, 12 Cursor CLI stores, 8 OpenCode sessions (3 tool calls). No Cursor IDE, Grok, grokbot or terva store on this machine.

Method decisions and why:
- Normalizers rather than raw JSON for Claude Code and Codex, so the numbers describe what the lake indexes. Raw reads only where the normalizer emits nothing: Codex `item_completed` inner calls and Cursor CLI parts. Both are labelled raw in the doc.
- Polling tools (sleep, wait, Monitor, TaskStop, AwaitShell...) are excluded from retry detection. Without the exclusion, Codex `sleep` alone made every Codex session a retry loop.
- Retry requires the same result hash as well as the same input hash. Input-only matching fired on 27% of Codex sessions, almost all of them polling a command whose output was changing; with the output hash it fires on 0% of Codex cells and 2.9% of Claude main sessions.
- Repeated read counts identical ranges with a reset on write. A path-only count fired on paging (Codex `sed -n` ranges, Cursor `Read` offsets).
- Context pressure keys on compactions, not token peaks. Peaks reach 470k (Claude) and 259k (Codex), so an absolute peak measures the model window, which events do not carry.
- 1,412 of 1,454 Claude cache-read collapses follow a 5-minute idle gap (cache expiry), so collapses count only without a gap, and only for Claude: Codex cached-input counts fall without a gap in 44% of sessions.

Acceptance criteria 1, 3 and 4 stay unticked:
- AC1 says "on the internal lake". The numbers come from the local transcript stores, as the substitution note records, and the doc describes the definitions rather than DuckDB queries, because the program was throwaway. A re-run on the lake should repeat it with the doc's definitions.
- AC3 and AC4 are recorded for every harness with local data. Cursor IDE, Grok, grokbot and terva had none, and Codex outside code mode is described from the normalizer code only.

Filed as drafts under the epic: TKT-01M61PSX6J "Normalize: project Codex code-mode inner tool calls" and TKT-01M61PT07Y "Normalize: project Cursor CLI tool-call and tool-result parts". Two smaller gaps are in the doc only: OpenCode drops the tool end time from state.time, and its local database schema differs from the export the normalizer reads (not checked against `opencode export`, which reads the live database). The epic also says OpenCode leaves is_error null; the normalizer sets it from state.status.

## Summary

Landed docs/session-diagnostics-calibration.md. It was measured on this workstation's local transcript stores (Claude Code, Codex, Cursor CLI and OpenCode) through lampi's normalizers, and reports aggregate numbers only.

Recommended defaults:
- large_tool_result (warn): one result of 64 KiB or more, or 10 or more of 32 KiB or more.
- large_tool_input (info): 32 KiB.
- error_streak (warn): 5.
- retry_loop (warn): 3 repeats of the same tool, input hash and result hash within 5 non-polling calls, or 3 retries of a failed call.
- repeated_read (info): 5 identical-range reads with no write between.
- context_pressure (info): 3 compactions, plus 3 non-idle cache-read collapses for Claude.
- orphan_call: 3, not counting a trailing call.
- orphan_result: 1, unless the session was resumed.
- unreadable_lines (error): 1.
- Proposed new codes: noisy_tool_origin (one MCP server or extension with 25% or more of the result bytes and 256 KiB or more) and unknown_tool (all 128 calls to a name the harness did not offer failed).

Key findings: harness caps (Claude Bash about 30 KiB, Codex cells about 39 KiB) decide which size thresholds can fire. Codex code mode and Cursor CLI keep their tool calls out of the normalized events; two draft tickets are filed under the epic. Latency is available for Claude Code (every call) and Codex (cells, and inner MCP durations), but not for Cursor CLI.

AC1, AC3 and AC4 stay open, as the last note explains: the corpus is local rather than the internal lake, and four harnesses had no local data.
