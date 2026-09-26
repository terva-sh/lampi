---
schema: 3
id: TKT-01M3FZ4TWRC6JXYZHHQEGTDAQS
title: "Recall: TestPagesNeverMixGenerations flakes on the CI runner"
type: bug
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area/search
  - area/ci
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-26T23:00:01Z
updated_at: 2026-09-26T23:00:01Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/e4a47e8c
  name: ""
extensions: {}
---

## Description

`TestPagesNeverMixGenerations` in internal/recall/events_test.go fails on the CI runner because the test runs out of time, not because anything is wrong. It needs 50 served pages and at least one reload within 10 s. On the loaded runner it managed 48 pages with 2954 reloads (main push CI after merge c7d699c), and 35 pages with 1741 reloads (PR #11, run 143). Every page it served was correct. The failure is only the `race not exercised` guard.

The writer republishes every 10 ms, so most reads come back as reloads, and on a slow runner too few pages complete before the deadline. It passes locally.

Possible fixes: lower the page floor (the property holds on any served page, so a few pages plus one reload is enough); slow the writer; or stop on a count of served pages and drop the wall-clock deadline.
