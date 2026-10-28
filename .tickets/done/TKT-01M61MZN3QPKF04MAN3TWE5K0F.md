---
schema: 3
id: TKT-01M61MZN3QPKF04MAN3TWE5K0F
title: "Tool origin: tell built-in tools from MCP servers and extensions"
type: task
status: done
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
updated_at: 2026-10-28T17:02:22Z
created_by:
  id: agent:claude-code/049834bc
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/049834bc-stats
  name: Claude Code local agent
extensions: {}
---

## Description

To tell a noisy MCP server or extension from a built-in tool, derive an origin from each tool name: built-in, MCP server (with its server name), plugin or extension (with its name), or unknown.

The rules are per harness and come from the calibration spike. Claude Code's `mcp__<server>__<tool>` is the known case. Keep the parser in one place, table-tested with real names from each harness, and expose the origin as a derived field the usage aggregates and the diagnosis can group by. Decide whether it is computed at query time from `tool_name` or stored as an index column; query time avoids another index rewrite.

## Acceptance criteria

- [x] A tool name maps to built-in, MCP server, plugin or extension, or unknown, with the server or extension name.
- [x] Rules exist for every harness whose naming the spike recorded, table-tested with real names.

## Implementation plan

- New file internal/recall/origin.go: `Origin{Kind, Name}` with kinds `builtin`, `mcp`, `plugin`, `extension`, `unknown`, and `ToolOrigin(harness, toolName string) Origin`.
- Rules per harness come from the calibration spike (TKT-01M61MZN0, docs/session-diagnostics-calibration.md). Known case: Claude Code `mcp__<server>__<tool>`, plugin servers `mcp__plugin_<plugin>_<server>__<tool>`.
- Compute at query time from `tool_name` rather than storing a column: it avoids a second index rewrite and lets rules change without a rebuild. The aggregation groups by `(harness, tool_name)` in SQL and folds into origins in Go, which is cheap because distinct names are few.
- Table tests with real name shapes (placeholders for private server names), one block per harness, plus unknown and empty names.

## Notes

**agent:claude-code/049834bc-stats** at 2026-10-28T17:02:22Z

Implemented as planned in internal/recall/origin.go: `Origin{Kind, Name, Server}` (JSON `kind`, `name`, `server`, the last two omitted when empty) and `ToolOrigin(harness, toolName)`, computed at query time. Kinds are `builtin`, `mcp`, `plugin`, `extension` and `unknown`; `OriginKinds` lists them for filter validation.

### Rules and why

- Every harness: `mcp__<server>__<tool>` is MCP with the server as Name. A missing server or tool is unknown.
- Claude Code: `mcp__plugin_<plugin>_<server>__<tool>` is `plugin`, Name the plugin and Server the server, split at the first `_` after `plugin_` (plugin names are kebab-case). A bare capitalised word is built in. Anything else is unknown, which is the calibration's "other" origin: names with a missing or extra prefix, every one of which failed.
- Codex: bare snake_case is built in; `<ns>.<tool>` and `<ns>:<tool>` (`clock.sleep`, `collab:wait`) are `extension` with Name the namespace. MCP is read from `mcp__` names, which the projection tickets TKT-01M61PSX (Codex inner calls) will write. Until then, an MCP call outside code mode is named by its bare tool and reads as built in.
- Cursor CLI: a bare capitalised word is built in; `mcp__cursor__<tool>` is built in, because `cursor` is the namespace Cursor uses for its own tools through CallDynamicTool. An unprojected `CallDynamicTool` is unknown, since it hides the tool it called.
- Other harnesses (OpenCode, Grok, terva, Cursor IDE): a bare identifier is built in. The spike recorded no MCP naming for them, so only the generic `mcp__` rule applies.

### Alternatives

- A list of built-in names per harness lost. Claude Code adds tools often, and a stale list would mark a new built-in tool as unknown. The shape rule marks the wrong-prefix names unknown without one.
- A stored index column lost, as the plan said: it needs another rebuild, and the rules are expected to change as the projections land.

Table test TestToolOrigin has one block per harness, using real built-in names and placeholder server names, plus empty and malformed names.

## Summary

ToolOrigin(harness, name) in internal/recall/origin.go places a tool name as builtin, mcp (server), plugin (plugin and server), extension (namespace) or unknown, at query time, with per-harness rules from the calibration spike and a table test per harness.
