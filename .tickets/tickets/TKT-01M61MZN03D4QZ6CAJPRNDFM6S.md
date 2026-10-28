---
schema: 3
id: TKT-01M61MZN03D4QZ6CAJPRNDFM6S
title: "Spike: calibrate tool size and session smell thresholds"
type: spike
status: in-progress
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
claim:
  actor: agent:claude-code/049834bc-spike
  branch: worktree-agent-a4af859388c33824c
  worktree: /home/sothr/workspace/git.local.sothr.com/terva-sh/lampi/.claude/worktrees/agent-a4af859388c33824c
  commit: 2439f8a0f31b8907b1905d5ac3ea5bd602ed46da
  session: null
  claimed_at: 2026-10-28T16:31:11Z
  expires_at: null
archive: null
created_at: 2026-10-28T16:20:09Z
updated_at: 2026-10-28T16:34:09Z
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
- [ ] Firing rates for each candidate smell at several thresholds are recorded, and thresholds are chosen.
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
