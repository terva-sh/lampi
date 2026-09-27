---
schema: 3
id: TKT-01M3JEKA6RBHK9WSGEZZKKQYHC
title: "Web UI: activity page with charts, tables and release C validation"
type: task
status: in-progress
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
claim:
  actor: agent:claude-code/e226d0e4
  branch: web/activity-page
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-e226d0e4
  commit: 9a7c61b5f5412262781a6b40f287879731e12232
  session: null
  claimed_at: 2026-09-27T22:28:35Z
  expires_at: null
archive: null
created_at: 2026-09-27T22:08:33Z
updated_at: 2026-09-27T22:28:55Z
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

- [x] Viewers see accepted-update and net logical-size charts with equivalent tables for each range preset and harness filter, and the page works without JavaScript.
- [x] Unmeasured buckets are visibly distinct from zero, and the page states coverage start, purge and restore limits and what the units are not.
- [x] An ingest-to-page test drives synthetic uploads through the lake API, including a retry and a purge, and checks the rendered table; backup and restore keep the history visible.
- [x] The browser smoke (internal/web/smoketest) covers the Activity page with keyboard navigation and a narrow viewport.
- [x] just ci and go test -race ./... pass; docs/web-dashboard.md, docs/architecture.md and docs/web-ui-plan.md describe the measurement, its units and its known omissions.

## Definition of done

- [x] Validation evidence and rationale are recorded; measurement and recovery behavior are documented.

## Implementation plan

1. internal/web/activity.go: `/activity` handler with range presets that end at the current bucket, so every preset is whole buckets under the cap. It calls `catalog.Activity` once and builds two single-series chart views and a table.
2. Charts are server-rendered SVG in templates/page.html: one y-axis per chart, bars capped at 24 units with a 2-unit gap, a rounded data end and a square baseline end, hairline grid, and a transparent full-height hit rect per bucket carrying a `<title>` hover label. Unmeasured runs are one hatched rect. A key appears only when needed (grew/shrank, not measured).
3. Colors come from the dataviz validator: #00846b and #c9651a pass the lightness, chroma, CVD, normal-vision and contrast checks on white. The dashboard's older #5e8e7e failed the chroma floor.
4. Mobile: charts keep a 600-unit minimum width inside their own scroller so the tick text stays readable, and lake.js scrolls them to the newest bucket.
5. Tests: `TestActivityFromIngestToPage` (the /v1 upload path, a retry, a grown transcript, purge, and a `VACUUM INTO` restore opened as a new lake) and `TestChartEdges`. The smoke fixture gets three weeks of synthetic head_updates rows plus a Playwright section.
6. Docs: web-dashboard.md Activity section, e2e/README.md, architecture.md, web-ui-plan.md status.

## Notes

**agent:claude-code/e226d0e4** at 2026-09-27T22:28:55Z

Choices made while building the page, and what was rejected.

- **Server-rendered SVG, not JavaScript charts.** The page draws without JavaScript and is tested from Go, which fits the existing dashboard (its harness chart is `<meter>`). A client-side chart library was rejected: the design rules out a Node build or CDN.
- **Two charts, not one chart with two y-axes.** Counts and bytes have unrelated scales. A second y-axis invites false comparisons between the two lines.
- **Byte ticks are binary-nice** (`niceBytes`: 1/2/5 × a power of 1024), so an axis reads +256 KiB rather than +195.3 KiB. The first render showed the decimal version.
- **Selects carry `aria-label`**, like the Sessions form. Without it, a select's accessible name includes the selected option, and the smoke's `getByLabel('Harness')` could not find it. Found by the smoke.
- **Mobile**: the first render scaled the whole SVG down to about 5px text. Now the chart scrolls horizontally inside its panel with a 600-unit minimum width. The page does not overflow, and the table carries the same numbers.
- **The smoke fixture writes `head_updates` rows directly**, rather than uploading and growing sessions. Uploads would change the session counts and normalization states that the rest of the smoke asserts. The ingest path itself is proven in Go by `TestActivityFromIngestToPage`.
- **Bad `/activity` parameters answer with the JSON 400** that the other pages use (`pageError`), not an HTML error page.

Validation, 2026-09-28: `GOFLAGS=-mod=mod just ci` green. `GOFLAGS=-mod=mod go test -race ./...` green, the whole module. `mise exec -- node e2e/web-smoke.mjs <playwright>` passed, including the new Activity checks (30-day and 7-day-hourly tables, a harness filter through the form, a hatched unmeasured range, a shrinking bar, mobile without page overflow, keyboard reaching the Range select, the 90-day view without JavaScript, and the empty lake). The screenshots were looked at on desktop and at 390px. They sit in the smoke's temporary evidence directory and are not committed.
