---
schema: 3
id: TKT-01M61MZNCF5QGBXEDDA0WWR97J
title: "CLI, API and MCP: query tool usage statistics"
type: task
status: in-progress
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
claim:
  actor: agent:claude-code/049834bc-surf
  branch: worktree-agent-a363e408d8ea73e0e
  worktree: /home/sothr/workspace/git.local.sothr.com/terva-sh/lampi/.claude/worktrees/agent-a363e408d8ea73e0e
  commit: c35ada00cf97e6216c36de9573117dfc6b89aadc
  session: null
  claimed_at: 2026-10-28T17:51:59Z
  expires_at: null
archive: null
created_at: 2026-10-28T16:20:09Z
updated_at: 2026-10-28T17:52:28Z
created_by:
  id: agent:claude-code/049834bc
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/049834bc-surf
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
- [ ] --context on the CLI, and context on the routes and tool_stats, return the starting-context hint (TKT-01M61MZNW) with its kind and note labels.
- [ ] An end-to-end test runs real Codex code-mode and Claude MCP transcripts through the normalizers, publish and index to the route.

## Implementation plan

- Shared handler in internal/web/tools.go: `parseToolStats(q)` (strict: unknown or repeated keys refused; an empty value means no filter, so a plain form can submit every field) and `(*Server).toolStats(ctx, scope, q)`, which answers 503 search_unavailable with no index, fills Scope and Now, and defaults the range to the last 7 days when no since, until or bucket is given. `all=true` asks for no range at all and is refused together with since, until or bucket. Parameters: harness, project, unlinked, model, tool, origin, origin_name, since, until, all, group_by, split, bucket, largest, context.
- Errors: failure() maps recall.ErrTooManyValues to 400 too_many_values, and a *recall.FilterError adds `param` to the error body, so an agent or the CLI can say which value was refused. mcpHints gains too_many_values.
- Read API: `GET /api/read/v1/tools` with events:read, registered beside the event stream when registrations are on. Audit line (events.read, "tools query=...") flushed before answering, TouchReadToken, scope from ReadTokenScope, Cache-Control no-store. The body is recall.ToolStats as is; links stay relative, as on the browser route.
- Browser API: `GET /api/web/v1/tools` via get(), scope from scopeOf(r).
- MCP: `tool_stats` with the route's parameters, booleans and integers typed. It names no session, so session_uid is refused rather than stripped. Largest-block links are made absolute. Audit detail comes from the schema, as for the other tools.
- CLI: `terva-lampi query tools`. Server and token resolution moves into a helper shared with `query events`. Output is JSONL with one key per line: a `summary` line (as_of, range, grouping, bucket_starts, totals, coverage), one `group` line per group, one `largest` line per block, and with --context one `context` line holding the whole hint with its kind and note. Totals and the context note go to stderr. --out is written only when complete, mode 0600.
- Tests: route parity (browser, read, MCP), scope (session-scoped and bay-scoped tokens), strict parsing, search off, audit; the CLI against an httptest lake; one end-to-end test that runs real Codex code-mode and Claude MCP transcripts through the normalizers, publish and index Pass.
- Docs: docs/cli.md, docs/web-api.md, docs/reading-the-lake.md.
