---
schema: 3
id: TKT-01M61NT6XQ5WA0XZ0AXZEYMHHG
title: "Dashboard: session statistics for tokens, errors and tools"
type: epic
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-28T16:34:39Z
updated_at: 2026-10-28T17:39:17Z
created_by:
  id: agent:claude-code/d4b24133
  name: ""
updated_by:
  id: agent:claude-code/d4b24133
  name: ""
extensions: {}
---

## Description

The overview and operations pages show counts of sessions, artifacts, machines and disk, but nothing about what happened inside the sessions. The owner wants token totals, error trends and tool use, overall and per harness, with time series and a harness filter. This came out of a conversation on 2026-10-28.

### What the lake already has

- Every normalized event carries `usage` (input, output, cache_read, cache_write, cost_usd) and `tool` (name, call_id, is_error); see `internal/normalize/event.go`. Claude, Codex, OpenCode, Cursor and terva fill usage.
- The search index `docs` table (`internal/recall/index.go`) has one row per event with `harness`, `event_type`, `tool_name`, `tool_error` and `recorded_ns`, indexed on tool and error. Tool and error statistics can come from it without new ingestion.
- `/activity` already draws hourly and daily charts with a harness filter; new charts should follow its pattern.

### Known traps

1. Usage is not in the index. Tokens need a rollup table, best written by the indexer, which already reads each session generation once.
2. Claude Code repeats the same `message.usage` on every content-block line of one API response, and `claude.go` emits a usage event for each. A plain sum over-counts. Deduplicate on the message id kept in `extra`.
3. Harnesses count input differently. Codex `last_token_usage.input_tokens` includes `cached_input_tokens`; Claude reports `cache_read_input_tokens` and `cache_creation_input_tokens` apart from `input_tokens`. Settle one definition before showing a total.
4. Tool results in Claude and Codex carry only `call_id`, so error rate by tool needs a join back to the call.
5. Tool names differ by harness (`Bash`, `shell`, `run_terminal_cmd`; `Edit`, `apply_patch`). Cross-harness views need a canonical vocabulary.

### Requirement from the owner

Wherever a harness reports it, keep cache read and cache write apart from plain input and output, all the way to the dashboard. Do not fold them into one input figure.

### Order

Tool and error work (children 3 and 4) does not depend on the token rollup and can go first. The token rollup carries the most design risk.

## Notes

**agent:claude-code/d4b24133** at 2026-10-28T17:39:17Z

2026-10-28: #215 (TKT-01M61MYZ6H6J8MGNPWHS8MTJ1X Session diagnostics, noisy tools and tool usage statistics) merged per-event size, call, model and token columns into search.db at schema 5 while this stack was open. The owner chose to layer the two: this epic's stats rollup becomes schema 6 on top of those columns and stays the source for token totals, time series and errors, because it counts each model call once and the raw columns overcount Claude by about 2x. Tool sizes, latency, largest blocks, tool origin and session diagnosis stay with the other epic. The Tools tab (#218) is kept and the other epic's dashboard ticket extends it. Notes recording this are on TKT-01M61MZN8YEFZ90AZDW62QG2DZ, TKT-01M61MZNFY69TNQJWH0PS2WPKS and TKT-01M61MZN3QPKF04MAN3TWE5K0F.
