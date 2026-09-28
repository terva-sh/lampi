---
schema: 3
id: TKT-01M3KC2DGFG4G7PZ909573RKEC
title: Status lines give an oldest age when no job is outstanding
type: chore
status: draft
status_reason: null
priority: low
due_on: null
labels:
  - area/server
  - area/agent
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-28T06:43:37Z
updated_at: 2026-09-28T06:43:37Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/e4a47e8c
  name: ""
extensions: {}
---

## Description

With no jobs outstanding, `terva-lampi status` prints
"lake_normalize_jobs: 0 outstanding, oldest queued 0s ago, …" and
`serve normalize --status` prints "jobs: 0 outstanding, queued or
running, oldest queued 0s ago". Both should say that none are
outstanding rather than give an age.
