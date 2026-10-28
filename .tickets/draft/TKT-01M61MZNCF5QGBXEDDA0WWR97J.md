---
schema: 3
id: TKT-01M61MZNCF5QGBXEDDA0WWR97J
title: "CLI, API and MCP: query tool usage statistics"
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
  - area/docs
assignees: []
milestone: null
parent: TKT-01M61MYZ6H6J8MGNPWHS8MTJ1X
origin: null
dependencies:
  - TKT-01M61MZN8YEFZ90AZDW62QG2DZ
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-28T16:20:09Z
updated_at: 2026-10-28T16:20:09Z
created_by:
  id: agent:claude-code/049834bc
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/049834bc
  name: Claude Code local agent
extensions: {}
---

## Description

Expose the tool usage aggregates where the rest of recall is exposed:

- CLI: `terva-lampi query tools` with `--since/--until`, `--harness`, `--project`, `--model`, `--by tool|origin|harness|model`, `--bucket hour|day`, and `--largest N` for the biggest blocks. JSONL out, like `query events --count-by`.
- Read API: a route under `/api/read/v1` (and the browser route under `/api/web/v1`) answering the same request.
- MCP: a `tool_stats` recall tool, so an agent can check which tools in its own sessions are costly.

All scoped to the token, all content-free.

## Acceptance criteria

- [ ] terva-lampi query tools answers the aggregates with the documented filters and groupings.
- [ ] Read and browser API routes and a tool_stats MCP tool answer the same requests.
- [ ] docs/cli.md, docs/web-api.md and docs/reading-the-lake.md describe them.
