---
schema: 3
id: TKT-01M61MZNRCQC8MHFF7RHT1J293
title: "CLI, API, MCP and dashboard: diagnose a session"
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
  - TKT-01M61MZNMZ0ZC0CG223990GMCH
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-28T16:20:10Z
updated_at: 2026-10-28T16:20:10Z
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
