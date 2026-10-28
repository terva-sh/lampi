---
schema: 3
id: TKT-01M61MZNRCQC8MHFF7RHT1J293
title: "CLI, API, MCP and dashboard: diagnose a session"
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
  - TKT-01M61MZNMZ0ZC0CG223990GMCH
  - TKT-01M61MZNFY69TNQJWH0PS2WPKS
blocks_on: none
references: []
claim:
  actor: agent:claude-code/049834bc-dsurf
  branch: worktree-agent-ade7e5ce999c8d3c8
  worktree: /home/sothr/workspace/git.local.sothr.com/terva-sh/lampi/.claude/worktrees/agent-ade7e5ce999c8d3c8
  commit: fb47214ed0d381f3d3c7a538552e1f3ed73d2629
  session: null
  claimed_at: 2026-10-28T18:27:33Z
  expires_at: null
archive: null
created_at: 2026-10-28T16:20:10Z
updated_at: 2026-10-28T18:27:55Z
created_by:
  id: agent:claude-code/049834bc
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/049834bc-dsurf
  name: Claude Code local agent
extensions: {}
---

## Description

Expose session diagnosis:

- CLI: `terva-lampi query diagnose SESSION`, and a lake-wide mode (`--since 7d`) that groups findings by code, harness and project.
- Read and browser API routes for one session's findings.
- MCP: a `diagnose_session` recall tool, so an agent can review its own past sessions.
- Dashboard: a findings panel on the session page, each finding linking to its events and showing its suggestion.

Decide whether findings are computed on request or cached per session generation; the lake-wide mode is the case that may need caching.

## Acceptance criteria

- [ ] terva-lampi query diagnose answers one session and a lake-wide grouped view.
- [ ] API routes and a diagnose_session MCP tool answer the same requests.
- [ ] The session page shows findings with links to their events and their suggestions.
- [ ] The compute-on-request versus cache decision is recorded with its reasons.

## Implementation plan

- recall: `DiagnoseLake(ctx, DiagnoseLakeRequest)` walks the most recently updated ready sessions in scope (catalog `RecentReadySessions`, newest last head update first, with since, harness and project filters, reading limit+1 to know there are more). It runs `Reader.Diagnose` on each and groups findings by code × harness × project_id: sessions, findings, events, and up to 3 example session uids. The limit defaults to 200, at most 1000. The walk stops 500 ms before the read context's deadline and reports `truncated` with reason `time`; more sessions than the limit is reason `limit`. A session purged, normalized again or no longer ready between listing and reading counts in `skipped`.
- web (internal/web/diagnosis.go): browser `GET /api/web/v1/sessions/{uid}/diagnosis` and `GET /api/web/v1/diagnosis`; read-token `GET /api/read/v1/sessions/{uid}/diagnosis` and `GET /api/read/v1/diagnosis` (events:read, token scope, audit before answering, no-store). The read-token prologue of readTools moves into one helper the three read routes share. The single-session body is the engine's Report with `links` per finding, parallel to `positions`: relative (EventLink) on the HTTP routes and absolute on MCP, as tool_stats does.
- MCP: `diagnose_session` (session_uid) and `diagnose_lake` (since, limit, harness, project), with the routes' bodies.
- CLI: `terva-lampi query diagnose SESSION_UID` (a summary line, then one finding line each) and `terva-lampi query diagnose [--since D] [--limit N] [--harness H] [--project ID]` (a summary line, then one group line each). The fetch and --out reuse the query tools helpers.
- Dashboard: a closed-by-default findings panel on the transcript page, computed on request under its own budget inside the page's read context. A failure or timeout leaves a one-line notice and the page still renders.
- Decision: compute on request; the lake-wide view is a capped walk; a per-generation cache is deferred until measured.
- Docs: cli.md, web-api.md (Session diagnosis, MCP table), reading-the-lake.md, web-dashboard.md, performance.md (measurements).
