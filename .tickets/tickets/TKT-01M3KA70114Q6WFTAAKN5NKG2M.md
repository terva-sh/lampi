---
schema: 3
id: TKT-01M3KA70114Q6WFTAAKN5NKG2M
title: "Normalization status: queue metrics, alert rules, /v1/stats"
type: task
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
  - area/ops
  - area/protocol
assignees: []
milestone: null
parent: TKT-01M3K45MQBRESGG5HEZZSZ3FDQ
origin: null
dependencies: []
blocks_on: none
references: []
claim:
  actor: agent:claude-code/e4a47e8c
  branch: ops/normalize-status
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-e4a47e8c
  commit: cb1279e51512c7ce8dbe4ed10547dd4c57e8622f
  session: null
  claimed_at: 2026-09-28T06:11:10Z
  expires_at: null
archive: null
created_at: 2026-09-28T06:11:09Z
updated_at: 2026-09-28T06:11:10Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/e4a47e8c
  name: ""
extensions: {}
---

## Description

The lake exposes how many sessions sit in each normalization state
(`lampi_sessions_by_normalization`, and the dashboard overview), but
not whether the queue is moving. On 2026-09-28, 71 of the hosted
lake's 92 sessions turned out to be "unknown": they had not been
normalized for their current head since before normalization
generations were tracked, and nothing raised it. The fix was
`serve normalize --all`.

### Metrics on /metrics

The in-memory queue and `normalize_jobs.enqueued_at` supply these:

- `lampi_normalize_jobs{state="queued|running|retrying"}` (gauge)
- `lampi_normalize_oldest_pending_age_seconds` (gauge)
- `lampi_normalize_jobs_total{result="ok|failed|retried"}` (counter)
- `lampi_normalize_duration_seconds` (histogram)
- `lampi_normalize_last_success_timestamp_seconds` (gauge)

### Alert rules

Document example Prometheus rules:

- any failed session
- any unknown session
- the oldest pending job older than 15 minutes
- pending above zero with no success in 30 minutes

### GET /v1/stats

Add a `normalization` object, behind the same device-token auth as the
other /v1 routes. It carries:

- per-state counts
- queued, running and retrying jobs
- the oldest pending age
- the last failure's session and message

`terva-lampi status` prints it as `lake_normalization:` lines.

### Storage sample after a batch

Take a storage sample when the normalize queue drains after a batch,
so the operations page does not keep showing derived-store sizes from
before a re-normalization until the next hourly sample.

## Acceptance criteria

- [ ] /metrics carries queue depth by state, oldest pending age, job results, duration and last success
- [ ] docs carry example alert rules for failed, unknown, stuck and stalled normalization
- [ ] GET /v1/stats returns a normalization object with counts, queue, oldest age and last failure
- [ ] terva-lampi status prints the lake's normalization state
- [ ] a storage sample is taken when the normalize queue drains after a batch
