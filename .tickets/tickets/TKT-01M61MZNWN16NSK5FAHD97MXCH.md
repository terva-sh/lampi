---
schema: 3
id: TKT-01M61MZNWN16NSK5FAHD97MXCH
title: "Tool usage: estimate starting-context cost of MCP servers"
type: task
status: blocked
status_reason: Surfaces and engine done; AC 3 needs Claude Code's offered tool list projected (TKT-01M61SPVY01PVEFT1N9JKBK4HB, draft).
priority: low
due_on: null
labels:
  - area/search
assignees: []
milestone: null
parent: TKT-01M61MYZ6H6J8MGNPWHS8MTJ1X
origin: null
dependencies:
  - TKT-01M61MZN3QPKF04MAN3TWE5K0F
  - TKT-01M61MZN8YEFZ90AZDW62QG2DZ
  - TKT-01M61MZMTSK36M0TB5ZZFF66RA
  - TKT-01M61SPVY01PVEFT1N9JKBK4HB
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-28T16:20:10Z
updated_at: 2026-10-28T18:09:05Z
created_by:
  id: agent:claude-code/049834bc
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/049834bc-surf
  name: Claude Code local agent
extensions: {}
---

## Description

A server's tool definitions take context at session start even when none of its tools is called, and transcripts generally do not record them. Estimate that cost from the first `usage` event's input plus cache tokens (the starting context), compared across sessions grouped by which servers appear.

This only shows a correlation and cannot see a server that is configured but never called, so present it as a hint beside the tool usage statistics, not as a finding. Check whether any harness records its tool list (Codex rollouts may) and use that where present.

## Acceptance criteria

- [x] Starting context tokens are compared across sessions grouped by the servers they use.
- [x] The result is labelled as a correlation hint, not a finding.
- [ ] Any harness that records its tool list is used directly.

## Implementation plan

Engine (done, internal/recall/toolcontext.go): `ToolStatsRequest.Context` adds `ToolStats.Context`, a `ToolContext` with `kind: "correlation_hint"`, a fixed `note`, and `min_sessions`. Its `harnesses[]` hold all sessions with a starting context per harness, and its `rows[]` hold one entry per (harness, origin) with `with`/`without` `{sessions, median_tokens}`, `difference_tokens` and `suppressed`.

- Starting context is the first usage row by position: input + cache_read + cache_write, or input alone for Codex, whose input already holds cached input (calibration doc). It is read in the same walk as the statistics, so it costs no extra query.
- A session uses an origin when it made at least one call (inner calls included) whose toolorigin is mcp, plugin or extension.
- Medians are exact and nearest-rank. A row is suppressed when either side has fewer than ToolContextMinSessions (5) sessions.
- Sessions: the ones the request walks. That means in scope and passing the harness and project filters, and with a range, accepted within a day of since. The tool, origin and model filters do not apply, because the comparison is between sessions.

Left for the surfaces agent: `--context` on `terva-lampi query tools`, `context=1` on the read and browser routes and the MCP tool, and a column or panel on the Tools page. Each must show the `kind` and `note` labels so the hint is not read as a finding.

Tool lists: Codex rollouts record none. Claude Code records offered tool names in attachment lines, which are not projected; TKT-01M61SPVY covers that.

## Notes

**agent:claude-code/049834bc-stats** at 2026-10-28T17:43:03Z

The engine part is implemented in 50c2d87, internal/recall/toolcontext.go, inside `(*Index).ToolStats` (request field `Context`, response field `context`). TestToolStatsContext covers it. It runs with 12 Claude Code sessions, 6 calling an MCP server: the medians differ and the difference is reported. Three Codex sessions with nothing to compare against are suppressed. A zero scope reads nothing. At 2,000 synthetic sessions, the context block added nothing measurable (2.34 s with it, 2.3 s without; docs/performance.md).

### Deviation

The plan read the first usage row through docs_tokens with a lookup per session. It now comes from the same walk as the statistics, which reads each session's usage rows when Context is set. A separate per-session probe would add a query, and the walk already visits every row.

### Tool lists (AC 3, unticked)

- Codex: rollouts record no tool list. On recent rollouts, by key names only, `session_meta` has base_instructions, cli_version, context_window, cwd, git, id, model_provider and similar, and `turn_context` has model, sandbox and approval settings. Neither has a tools or servers field.
- Claude Code does record offered tools. Its transcripts carry `attachment` lines of type `deferred_tools_delta` (`addedNames`, `removedNames`, and MCP server status lists) and `prompt_snapshot` (with a `tools` key). The normalizer projects these as unknown events and keeps the payload only in `extra`, which the index does not store. So the engine cannot use them yet. I filed TKT-01M61SPVY01PVEFT1N9JKBK4HB, "Project Claude Code's offered tool list for the context hint" (draft), for that.

### What is left on this ticket

The surfaces: `--context` on the CLI, the route and MCP argument, and the Tools page column. These were left to the surfaces agent at the coordinator's request. TKT-01M61MZNC and TKT-01M61MZNF do not mention the context block, so this ticket goes back to ready for that work rather than to done.

**agent:claude-code/049834bc-surf** at 2026-10-28T18:09:05Z

The surfaces are done: `terva-lampi query tools --context` (a `context` JSONL line holding the whole block, and "context is a correlation hint: <note>" on stderr), `context=true` on GET /api/web/v1/tools and /api/read/v1/tools and on the MCP tool_stats tool (f12b6e4, tests in internal/web/tools_test.go, mcp_test.go and internal/cli/query_test.go), and the "Starting context by origin" panel on the Tools page with a "Correlation hint, not a finding" badge, the engine's note, per-harness medians, and "too few sessions" for suppressed rows (6b7c04b). Each surface carries `kind` and `note`. Documented in docs/web-api.md (Tool usage), docs/cli.md, docs/reading-the-lake.md and docs/web-dashboard.md.

AC 3 (use a recorded tool list) stays unticked: it waits on TKT-01M61SPVY01PVEFT1N9JKBK4HB, "Project Claude Code's offered tool list for the context hint", still a draft. So this ticket goes to blocked on that rather than done.

**agent:claude-code/049834bc-surf** at 2026-10-28T18:09:05Z

in-progress to blocked: Surfaces and engine done; AC 3 needs Claude Code's offered tool list projected (TKT-01M61SPVY01PVEFT1N9JKBK4HB, draft).

## Summary

Engine part landed in 50c2d87: ToolStatsRequest.Context returns a correlation_hint block comparing median starting context of sessions that called each MCP, plugin or extension origin with those that did not, per harness, suppressed under 5 sessions a side. Left: the --context CLI flag, the route and MCP argument, and the Tools page column (surfaces agent), and using Claude Code's recorded tool list (TKT-01M61SPVY).
