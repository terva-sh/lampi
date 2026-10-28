---
schema: 3
id: TKT-01M61MZN8YEFZ90AZDW62QG2DZ
title: Tool usage aggregates over the search index
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/search
assignees: []
milestone: null
parent: TKT-01M61MYZ6H6J8MGNPWHS8MTJ1X
origin: null
dependencies:
  - TKT-01M61MZMR6EHF8BAC5F4VNK9VN
  - TKT-01M61MZMTSK36M0TB5ZZFF66RA
  - TKT-01M61MZN3QPKF04MAN3TWE5K0F
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-28T16:20:09Z
updated_at: 2026-10-28T16:20:09Z
created_by:
  id: agent:claude-code/049834bc
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/049834bc
  name: Claude Code local agent
extensions: {}
---

## Description

Aggregate tool use over the search index, so the lake can say which tools are used most, when, and at what cost.

Per tool and per origin (MCP server, plugin, built-in), within filters for harness, project, model, bay scope and a time range:

- calls, results, sessions that used it
- error count and rate, where `is_error` is known
- input and output size: total, p50, p95, max, and the share of a session's tool result bytes
- call-to-result latency p50, p95, max, where both events have harness timestamps
- counts bucketed by hour or day, following the bucket rules in internal/catalog/activity.go
- the top N largest single blocks, as session, position, tool and size

Percentiles over SQLite need care at lake scale; measure on the internal lake and bound the work per request like the existing count limits.

## Acceptance criteria

- [ ] Calls, results, sessions, error rate, size and latency percentiles are computed per tool and per origin.
- [ ] Results filter by harness, project, model, range and token scope, and bucket by hour or day.
- [ ] The top N largest blocks are listed by session, position, tool and size.
- [ ] Per-request work is bounded and measured on the internal lake.
