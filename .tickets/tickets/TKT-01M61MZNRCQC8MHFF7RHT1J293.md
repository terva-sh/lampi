---
schema: 3
id: TKT-01M61MZNRCQC8MHFF7RHT1J293
title: "CLI, API, MCP and dashboard: diagnose a session"
type: task
status: ready
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
claim: null
archive: null
created_at: 2026-10-28T16:20:10Z
updated_at: 2026-10-28T16:35:45Z
created_by:
  id: agent:claude-code/049834bc
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/049834bc
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

- CLI: `terva-lampi query diagnose SESSION_UID` prints findings as JSONL. `--since D` without a uid gives the lake-wide grouped view (code × harness × project counts with example sessions).
- Read API `GET /api/read/v1/sessions/{uid}/diagnosis` and browser `GET /api/web/v1/sessions/{uid}/diagnosis`. Lake-wide `GET .../diagnosis?since=` caps the sessions it walks and reports `truncated`.
- MCP `diagnose_session` (session_uid) mirrors the route. Same test pattern as tool_stats.
- Dashboard: a findings panel on the transcript page (pages.go transcriptPage), each finding linking to its event anchors and showing its suggestion. Computed on request, with a 5s budget.
- Decision to record: compute on request for one session, since a single pass over one events file is cheap and always current. The lake-wide view walks at most N recent sessions per request. A per-generation cache is deferred until measurements show the walk is too slow.
- Docs: cli.md, web-api.md, reading-the-lake.md, web-dashboard.md.
