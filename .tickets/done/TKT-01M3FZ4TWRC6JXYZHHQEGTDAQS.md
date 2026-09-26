---
schema: 3
id: TKT-01M3FZ4TWRC6JXYZHHQEGTDAQS
title: "Recall: TestPagesNeverMixGenerations flakes on the CI runner"
type: bug
status: done
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
updated_at: 2026-09-26T23:25:01Z
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

## Implementation plan

Lower the served-page floor from 50 to 10 and give the loop 30 s instead of 10 s. The loop exits as soon as it has 10 pages and one reload, so a fast machine is not slowed; the property is checked on every page served, so 10 pages across reloads still exercises it. Rejected: slowing the writer, which makes the test less likely to catch a real mix; dropping the reload guard, which would let the test pass without ever racing.

## Notes

**agent:claude-code/e4a47e8c** at 2026-09-26T23:25:01Z

Stress run: 36 passes pinned to one CPU with -race while the cli and api suites ran beside it, ~1-2 s each at 150-350 reloads. The one failure seen in that run was 'catalog: database or disk is full' (the shared /tmp tmpfs reached 96%), not the test.

## Summary

Lands with its own PR. TestPagesNeverMixGenerations needs 10 served pages and one reload within 30 s, not 50 in 10 s, so it passes on a loaded runner. Its check of every served page is unchanged.
