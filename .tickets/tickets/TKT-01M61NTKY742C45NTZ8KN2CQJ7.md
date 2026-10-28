---
schema: 3
id: TKT-01M61NTKY742C45NTZ8KN2CQJ7
title: "Usage over time: hourly and daily tokens by harness"
type: task
status: in-progress
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
claim:
  actor: agent:claude-code/d4b24133
  branch: stats/usage-over-time
  worktree: /home/sothr/.t3/worktrees/lampi/t3-d4b24133
  commit: ee0a91ef88da95241a5966d890f1ca170a616aff
  session: null
  claimed_at: 2026-10-28T16:53:48Z
  expires_at: null
archive: null
created_at: 2026-10-28T16:34:52Z
updated_at: 2026-10-28T16:54:36Z
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

- [x] Charts and table keep the four token kinds as separate series
- [x] Harness filter and range presets match /activity

## Implementation plan

Add Index.Series over the stats table from the usage rollup (SUM by bucket of whole hours, harness and scope filters, every bucket returned). New /usage page with Activity's range presets and resolver: range totals, and one chart per token kind with buildChart rather than a stacked chart, because cache reads are about 100x input and output and a stack would hide them (dataviz form choice; no new palette needed). A bucket with calls that did not report a kind is hatched; a bucket with no calls is zero. The harness filter lists the harnesses in the viewer's totals, because catalog.validHarness refuses grokbot (filed as its own bug).
