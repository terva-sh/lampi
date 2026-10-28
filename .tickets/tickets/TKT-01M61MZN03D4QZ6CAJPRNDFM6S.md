---
schema: 3
id: TKT-01M61MZN03D4QZ6CAJPRNDFM6S
title: "Spike: calibrate tool size and session smell thresholds"
type: spike
status: ready
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
updated_at: 2026-10-28T16:29:50Z
created_by:
  id: agent:claude-code/049834bc
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/049834bc
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
