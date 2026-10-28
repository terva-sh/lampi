---
schema: 3
id: TKT-01M61MZNFY69TNQJWH0PS2WPKS
title: "Dashboard: tool usage page with largest blocks"
type: task
status: done
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
updated_at: 2026-10-28T18:08:57Z
created_by:
  id: agent:claude-code/049834bc
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/049834bc-surf
  name: Claude Code local agent
extensions: {}
---

## Description

A dashboard page for tool usage: most-used tools and origins, calls over time, error rate, size and latency percentiles, and a table of the largest blocks with links into the transcript at the event's position. Filters for harness, project, model and range, matching the API.

The page should make the noisy-server case obvious: an origin's share of tool result bytes beside its share of calls.

## Acceptance criteria

- [x] The page shows most-used tools and origins, calls over time, error rate, size and latency.
- [x] The largest blocks link into the transcript at the event.
- [x] An origin's share of tool result bytes is shown beside its share of calls.

## Implementation plan

- `/tools` in internal/web/tools_page.go, registered in pageRoutes, with `tools` and `tools-form` template blocks, a nav link and a lead line. Data comes from `(*Server).toolStats`, the function behind the browser route, with the browser session's scope, largest=20 and context=true.
- Ranges reuse the Activity presets (so the calls chart stays inside a bucket's widest range) plus "All recorded time", which has no chart. Default 7d, as the API.
- Sections: a status line with totals and (admins) index coverage; the calls chart via buildChart; "By origin" folded from the groups in Go, with share of calls beside share of result bytes as meters, shares taken within the context or inner class as the engine does; the per-tool or per-origin table (first 100 groups); the largest blocks with the engine's EventLink into the transcript; the starting-context hint labelled "Correlation hint, not a finding"; notes on what the numbers are.
- States: no index 503, refused filter 400 in page, too many rows or a timeout in page, unknown or repeated parameter 400 like the other pages. No polling: a large range costs seconds.
- Tests in internal/web/tools_test.go; smoke fixture gains one MCP call per transcript, and e2e/web-smoke.mjs a Tools check. Docs: docs/web-dashboard.md, Tools.

## Notes

**agent:claude-code/049834bc-surf** at 2026-10-28T18:08:56Z

Implemented in 6b7c04b.

### Decisions

- Origin shares are folded from the groups in Go rather than a second ToolStats call grouped by origin. Groups already carry their origin, so one read answers both tables, and the noisy-server comparison shows under either grouping.
- The starting-context hint is always requested on the page. The engine author measured no cost for it (TKT-01M61MZNW note), and a checkbox would hide the hint TKT-01M61MZNW asked to show.
- The page does not poll (Activity does): a whole-lake range is seconds of reading.
- Ranges reuse activityRanges plus "all", so the chart's bucket range is always one the engine accepts.

### Browser smoke

The full e2e/web-smoke.mjs does not pass on this branch, and did not before it:
1. "mobile transcript overflows": the review count's sr-only spans in the mobile nav are absolutely positioned outside the scrolling nav, so every mobile page scrolled sideways. Reproduced with the base template; fixed with `position:relative` on the mobile `.topbar nav` (lake.css).
2. After that, the script waits for "Searching N of M ready sessions", which only an admin sees since coverage became admin-only; the fixture's default identity is not an admin. Not fixed here: it is unrelated to Tools.

The Tools section was run on its own (the script's login prologue plus the new Tools block) with Playwright 1.58.2 and Chromium against the fixture: passed, including the largest-block link focusing its event, Group by origin, and no mobile overflow. Screenshots looked right.

## Summary

/tools shows tool usage for the Activity ranges or all time, filtered by harness, origin, project and model: origin share of calls beside share of result bytes, a per-tool or per-origin table with error rate, sizes and latency, calls over time, the 20 largest blocks linked to their transcript events, and the starting-context correlation hint. Tests cover rendering, bay scope, empty, refused and search-off states; the Tools browser smoke passes in isolation (the full smoke has an unrelated pre-existing search-coverage failure).
