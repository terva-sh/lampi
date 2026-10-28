---
schema: 3
id: TKT-01M61NTM0K2TQHX28T2R4HN0TN
title: "Error trends: error events and failed tool results over time"
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/search
  - area/server
assignees: []
milestone: null
parent: TKT-01M61NT6XQ5WA0XZ0AXZEYMHHG
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-28T16:34:53Z
updated_at: 2026-10-28T21:24:00Z
created_by:
  id: agent:claude-code/d4b24133
  name: ""
updated_by:
  id: agent:claude-code/d4b24133
  name: ""
extensions: {}
---

## Description

Show error trends from data the lake already indexes.

- Count `error` events and tool results with `is_error` true, by hour or day, with a harness filter. Both kinds are in the search index (`event_type`, `tool_error`, `recorded_ns`).
- List the sessions with the most errors in the range, linking to the session page.
- Join a tool result to its call by `call_id` within the session to name the failing tool, because Claude and Codex results carry no tool name.
- Unreadable-line errors written by the normalizer are lake problems, not session errors; count them apart or leave them out.

## Acceptance criteria

- [x] Errors by hour or day with a harness filter
- [x] Failed tool results are named by joining on call_id

## Implementation plan

Read the stats rollup; no new ingestion. Index.Breakdown sums a kind by harness and name over a range (error raw types, failed tools), and Index.TopSessions ranks sessions by errors plus failed tool results, labelled through Catalog.DashboardSession so scope holds. A /usage/errors tab beside Tokens shares the range and harness form: charts of error events and failed tool results per bucket (from Series), the sessions with the most, and tables of error kinds and failing tools. Failed results are named through their call id at index time. Unreadable lines are lake faults and are left out. Sub-tabs under Usage rather than a new nav item, because the top nav already overflows at 1280px.

## Summary

Landed in #217. /usage/errors charts error events, failed tool results and unreadable lines over time, lists them by raw type and by failing tool name (failed results are named through their call_id), and lists the sessions with the most errors. Same range and harness form as the Tokens tab.
