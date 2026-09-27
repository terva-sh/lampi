---
schema: 3
id: TKT-01M3JEKA6RBHK9WSGEZZKKQYHC
title: "Web UI: activity page with charts, tables and release C validation"
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
  - area/docs
  - area/ci
assignees: []
milestone: null
parent: TKT-01M3F2RKCZZNB6C1EGEG1FDCQH
origin: null
dependencies:
  - TKT-01M3F2RKKRM1MP6GJ0P5BJ3JW7
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-27T22:08:33Z
updated_at: 2026-09-27T22:08:39Z
created_by:
  id: agent:claude-code/e226d0e4
  name: ""
updated_by:
  id: agent:claude-code/e226d0e4
  name: ""
extensions: {}
---

## Description

### Scope and rationale

Add an Activity page to the dashboard that draws the activity API (TKT-01M3F2RKK) as two charts, accepted updates and net logical head-size change, each beside an equivalent table. This ticket closes release C: end-to-end validation from ingest to chart, operator documentation, and the design record. Split from the original chart ticket during grooming on 2026-09-28.

### Contract

- `GET /activity`, viewer role, linked from the main navigation. Controls are a GET form: range presets (last 24 hours and 7 days hourly; last 7, 30 and 90 days daily) and harness. It works without JavaScript; the page takes part in the existing 25-second visible-tab refresh.
- Charts are inline SVG rendered by the Go template from the same catalog call the API uses, with a text alternative. Negative net change draws below the axis and is labelled as a logical change, never as storage saved.
- Buckets with coverage `none` are drawn as unmeasured (hatched or absent), not as zero, and the table says "not measured". The page states the coverage start, that purging a session removes its history from past buckets, that a restore rewinds history to the backup, and that these are accepted head updates, not network traffic, disk growth or user activity.
- An empty lake and a lake with coverage but no updates each have an explicit empty state.

## Acceptance criteria

- [ ] Viewers see accepted-update and net logical-size charts with equivalent tables for each range preset and harness filter, and the page works without JavaScript.
- [ ] Unmeasured buckets are visibly distinct from zero, and the page states coverage start, purge and restore limits and what the units are not.
- [ ] An ingest-to-page test drives synthetic uploads through the lake API, including a retry and a purge, and checks the rendered table; backup and restore keep the history visible.
- [ ] The browser smoke (internal/web/smoketest) covers the Activity page with keyboard navigation and a narrow viewport.
- [ ] just ci and go test -race ./... pass; docs/web-dashboard.md, docs/architecture.md and docs/web-ui-plan.md describe the measurement, its units and its known omissions.

## Definition of done

- [ ] Validation evidence and rationale are recorded; measurement and recovery behavior are documented.
