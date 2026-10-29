---
schema: 3
id: TKT-01M61MZNFY69TNQJWH0PS2WPKS
title: "Dashboard: extend the Usage Tools tab with sizes, latency and origins"
type: task
status: in-progress
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
claim:
  actor: agent:claude-code/049834bc
  branch: t3/diag-tools-tab
  worktree: /home/sothr/.t3/worktrees/lampi/t3-049834bc
  commit: c7ae2c2e35dab5e44ed4e4ea45f1414eae48014c
  session: null
  claimed_at: 2026-10-29T00:37:28Z
  expires_at: null
archive: null
created_at: 2026-10-28T16:20:09Z
updated_at: 2026-10-29T00:44:08Z
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

- [x] The page shows most-used tools and origins, calls over time, error rate, size and latency.
- [x] The largest blocks link into the transcript at the event.
- [x] An origin's share of tool result bytes is shown beside its share of calls.

## Implementation plan

### Revised 2026-10-28

The owner chose to keep the Tools tab from PR #218 (`/usage/tools`, from TKT-01M61NT6XQ5WA0XZ0AXZEYMHHG) and extend it from this ticket rather than build a second page. The separate `/tools` page built here was removed from PR #223 before it merged; its code is at 528c22a (`internal/web/tools_page.go`, the `tools` and `tools-form` templates), for reuse.

Once #218 is on main, extend the tab with what the stats rollup does not cover, from `(*recall.Index).ToolStats`:

- Each origin's share of calls beside its share of result bytes. `toolorigin` gives the origin, and #218's `mcp:<server>` category rule should move into it.
- Size p50, p95 and max, and latency p50 and p95, per tool or category.
- The largest blocks, linked to their transcript events.
- The starting-context hint, labelled as a correlation.

Keep the tab's shared range and harness form, and add an origin or group-by control only if it fits that form. Run the work through the tab's own data path, together with the rollup, inside one 5 s read.

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

**agent:claude-code/049834bc** at 2026-10-28T18:54:31Z

Coordinator: renamed this branch's internal/web/tools.go, tools_page.go and tools_test.go to toolstats.go, toolstats_page.go and toolstats_test.go; toolRow and toolsView to toolStatsRow and toolStatsView; and the page's table class to toolstats-table. PR #218 (Tool usage: a Tools tab with a cross-harness map, from TKT-01M61NT6XQ5WA0XZ0AXZEYMHHG) adds files and names that collide with these. The owner decided the two epics layer. /usage/tools groups calls by category from the stats rollup; /tools shows origin, sizes, latency and largest blocks. The dashboard will then show both a top-level Tools page and a Tools tab under Usage, a navigation choice left to the owner.

**agent:claude-code/d4b24133** at 2026-10-28T17:39:17Z

From TKT-01M61NT6XQ5WA0XZ0AXZEYMHHG, 2026-10-28. The owner chose to keep the Tools tab from PR #218 and extend it from this ticket rather than build a second page.

`/usage/tools` already shows tool calls and failed results over time, categories with calls, failures, failure rate and the names counted, and a category by harness matrix, all from the stats rollup. It shares the range and harness form with the Tokens and Errors tabs. Sizes, latency percentiles, the largest blocks and an origin's share of result bytes are what this ticket adds, from the docs columns. The tab lives in `internal/web/tools.go` and the `tools-tab` template.

**agent:claude-code/049834bc** at 2026-10-28T18:58:14Z

done to in-progress: The owner chose to extend PR #218's Usage Tools tab instead of a second page; the /tools page was removed from PR #223.

**agent:claude-code/049834bc** at 2026-10-28T18:58:14Z

in-progress to blocked: Extends the /usage/tools tab from PR #218 (TKT-01M61NT6XQ5WA0XZ0AXZEYMHHG), which is not on main yet.

**agent:claude-code/049834bc** at 2026-10-29T00:37:28Z

Unblocked: #218 merged to main (2b8d5f2). Picking up the revised plan.

**agent:claude-code/049834bc** at 2026-10-29T00:44:08Z

Built on #218's tab as the revised plan says. Category placement now goes through toolorigin.Of(harness, name): mcp:SERVER and plugin:PLUGIN, and Cursor CLI's older mcp__cursor__X reads by its bare name (it was mcp:cursor). The engine read (ToolStats, no bucket, largest 20, context on) shares the tab's 5 s readContext with the rollup; ErrTooManyValues or a deadline shows a notice in place of the detail panels instead of failing the page. Calls over time and the error rate come from the existing rollup charts and category table, so the engine is not asked for buckets. No origin or group-by control was added: the form stays range and harness, as the plan allowed. Alternative rejected: per-category size percentiles, which cannot be merged from per-tool percentiles; sizes and latency are per tool instead.

## Summary

The Usage Tools tab gains By origin (share of calls beside share of result bytes), Result sizes and latency per tool, Largest blocks linked to their events, and the starting-context correlation hint, read from the tool statistics engine for the tab's range and harness. Categories use toolorigin for MCP and plugin tools.
