---
schema: 3
id: TKT-01M61NTM2ZDBCKGA08BJKYM4Q1
title: "Tool usage: top tools, failure rates and a cross-harness map"
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/search
  - area/server
assignees: []
milestone: null
parent: TKT-01M61NT6XQ5WA0XZ0AXZEYMHHG
origin: null
dependencies:
  - TKT-01M61NTM0K2TQHX28T2R4HN0TN
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-28T16:34:53Z
updated_at: 2026-10-28T21:47:21Z
created_by:
  id: agent:claude-code/d4b24133
  name: ""
updated_by:
  id: agent:claude-code/d4b24133
  name: ""
extensions: {}
---

## Description

Show which tools are used most, how often each fails, and how tools compare across harnesses.

- Add a canonical tool vocabulary (for example shell, edit, read, write, search, web, task, mcp:<server>) with a mapping table from each harness's tool names. Apply it when reading, so changing the map needs no reprojection. Unmapped names show as themselves.
- Views: top tools by call count, failure rate per tool (using the call_id join from the error-trends ticket), and a harness by tool matrix.
- Filter by harness and time range like `/activity`.

## Acceptance criteria

- [x] Canonical tool map applied at read time
- [x] Top tools, failure rate per tool and harness by tool matrix

## Implementation plan

### Approach

A Tools tab under Usage, beside Tokens and Errors, reading the stats rollup through `Index.Breakdown` (tool_call and tool_failed by harness and name) and `Index.Series` (tool calls per bucket).

### Canonical tool map

One case-insensitive table from a tool name to a category, in `internal/web/tools.go`, applied when the page is read. A change to the map needs no reprojection. Names the map does not know show as themselves, so nothing is hidden.

Categories: shell, read, write, edit, search, web, agents, plan, ask, script, and `mcp:<server>` for names of the form `mcp__server__tool`.

The names come from local transcripts on 2026-10-28 (the newest 150 sessions per harness):
- claude: Bash 80k, then Write, Read, Agent, Edit, SendMessage, AskUserQuestion, Monitor, WebFetch, WebSearch, and many mcp__* tools.
- codex: exec 28k, send_message, sleep, wait_agent, request_user_input_async, followup_task, list_agents, spawn_agent. Older Codex used shell, exec_command and apply_patch.
- terva: bash, ask_user_question, terva_status, read, memory, grep, write.

Codex `exec` is a code-mode script whose body calls other tools (mostly `tools.exec_command`), so it maps to script, not shell. One script call can hold many commands, and the map can't see inside it.

Rejected:
- A per-harness map. The same names mean the same thing across harnesses here, and one table is easier to read and extend. A harness-specific override can be added if a collision appears.
- Mapping at index time. It would need a reprojection for every map change, and it would lose the raw name.

### Views

- Totals: tool calls, distinct tools, failed results, failure rate.
- A chart of tool calls per bucket.
- Categories by calls, with failed results, failure rate, and the raw names under each.
- A category by harness matrix, which is the cross-harness view.

## Summary

Landed in #218. /usage/tools charts tool calls and failed tool results over time, lists categories busiest first with calls, failures, failure rate and every harness and name counted (the six busiest shown, the rest folded), and shows a category by harness matrix. Names map to categories at read time in internal/web/tools.go (shell, script, read, write, edit, search, web, agents, plan, ask, and mcp:<server>). The session diagnostics epic's dashboard ticket extends this tab with sizes, latency and origin.
