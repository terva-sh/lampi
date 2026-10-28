---
schema: 3
id: TKT-01M61MZNFY69TNQJWH0PS2WPKS
title: "Dashboard: tool usage page with largest blocks"
type: task
status: draft
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

A dashboard page for tool usage: most-used tools and origins, calls over time, error rate, size and latency percentiles, and a table of the largest blocks with links into the transcript at the event's position. Filters for harness, project, model and range, matching the API.

The page should make the noisy-server case obvious: an origin's share of tool result bytes beside its share of calls.

## Acceptance criteria

- [ ] The page shows most-used tools and origins, calls over time, error rate, size and latency.
- [ ] The largest blocks link into the transcript at the event.
- [ ] An origin's share of tool result bytes is shown beside its share of calls.
