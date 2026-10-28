---
schema: 3
id: TKT-01M61MZNFY69TNQJWH0PS2WPKS
title: "Dashboard: tool usage page with largest blocks"
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
assignees: []
milestone: null
parent: TKT-01M61MYZ6H6J8MGNPWHS8MTJ1X
origin: null
dependencies:
  - TKT-01M61MZNCF5QGBXEDDA0WWR97J
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

A dashboard page for tool usage: most-used tools and origins, calls over time, error rate, size and latency percentiles, and a table of the largest blocks with links into the transcript at the event's position. Filters for harness, project, model and range, matching the API.

The page should make the noisy-server case obvious: an origin's share of tool result bytes beside its share of calls.

## Acceptance criteria

- [ ] The page shows most-used tools and origins, calls over time, error rate, size and latency.
- [ ] The largest blocks link into the transcript at the event.
- [ ] An origin's share of tool result bytes is shown beside its share of calls.

## Implementation plan

- Server-rendered page `/tools` in internal/web (new tools.go beside activity.go): add to pageRoutes, pageData, the nav in templates/page.html, and `tools`/`tools-form` template blocks.
- Data from the same ToolStats call the API uses, scoped by the browser session.
- Content: a form (range, harness, project, model, group by tool or origin), a table of groups (calls, sessions, error rate, p50/p95/max size, share of result bytes beside share of calls, p50/p95 latency), a calls-over-time chart reusing buildChart, and a largest-blocks table linking to `/sessions/{uid}#e{pos}` (match the transcript page's anchor scheme).
- Tests in internal/web for rendering, scope, and empty and search-off states. Extend internal/web/smoketest and e2e/web-smoke.mjs with a Tools check, and run the browser smoke test locally (e2e/README.md).
- Docs: docs/web-dashboard.md, a Tools section.
