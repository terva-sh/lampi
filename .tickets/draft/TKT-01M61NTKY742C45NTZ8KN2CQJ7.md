---
schema: 3
id: TKT-01M61NTKY742C45NTZ8KN2CQJ7
title: "Usage over time: hourly and daily tokens by harness"
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/catalog
  - area/server
assignees: []
milestone: null
parent: TKT-01M61NT6XQ5WA0XZ0AXZEYMHHG
origin: null
dependencies:
  - TKT-01M61NTE24T6CZN1P92AV7164F
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-28T16:34:52Z
updated_at: 2026-10-28T16:34:53Z
created_by:
  id: agent:claude-code/d4b24133
  name: ""
updated_by:
  id: agent:claude-code/d4b24133
  name: ""
extensions: {}
---

## Description

Chart tokens by hour or by day with a harness filter, in the style of `/activity`.

- Store usage in hourly buckets by harness in the rollup from the usage-rollup ticket, so a range query does not reread sessions.
- Each chart and table keeps plain input, output, cache read and cache write as separate series (owner requirement). A stacked view and a cache hit ratio (cache read over all input) are both useful.
- Bucket on the event's `recorded_at`, not ingestion time.

## Acceptance criteria

- [ ] Charts and table keep the four token kinds as separate series
- [ ] Harness filter and range presets match /activity
