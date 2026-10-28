---
schema: 3
id: TKT-01M61MZN3QPKF04MAN3TWE5K0F
title: "Tool origin: tell built-in tools from MCP servers and extensions"
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/search
assignees: []
milestone: null
parent: TKT-01M61MYZ6H6J8MGNPWHS8MTJ1X
origin: null
dependencies:
  - TKT-01M61MZN03D4QZ6CAJPRNDFM6S
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-28T16:20:09Z
updated_at: 2026-10-28T16:35:44Z
created_by:
  id: agent:claude-code/049834bc
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/049834bc
  name: Claude Code local agent
extensions: {}
---

## Description

To tell a noisy MCP server or extension from a built-in tool, derive an origin from each tool name: built-in, MCP server (with its server name), plugin or extension (with its name), or unknown.

The rules are per harness and come from the calibration spike. Claude Code's `mcp__<server>__<tool>` is the known case. Keep the parser in one place, table-tested with real names from each harness, and expose the origin as a derived field the usage aggregates and the diagnosis can group by. Decide whether it is computed at query time from `tool_name` or stored as an index column; query time avoids another index rewrite.

## Acceptance criteria

- [ ] A tool name maps to built-in, MCP server, plugin or extension, or unknown, with the server or extension name.
- [ ] Rules exist for every harness whose naming the spike recorded, table-tested with real names.

## Implementation plan

- New file internal/recall/origin.go: `Origin{Kind, Name}` with kinds `builtin`, `mcp`, `plugin`, `extension`, `unknown`, and `ToolOrigin(harness, toolName string) Origin`.
- Rules per harness come from the calibration spike (TKT-01M61MZN0, docs/session-diagnostics-calibration.md). Known case: Claude Code `mcp__<server>__<tool>`, plugin servers `mcp__plugin_<plugin>_<server>__<tool>`.
- Compute at query time from `tool_name` rather than storing a column: it avoids a second index rewrite and lets rules change without a rebuild. The aggregation groups by `(harness, tool_name)` in SQL and folds into origins in Go, which is cheap because distinct names are few.
- Table tests with real name shapes (placeholders for private server names), one block per harness, plus unknown and empty names.
