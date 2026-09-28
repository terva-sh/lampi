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
updated_at: 2026-09-28T06:17:42Z
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

- [x] /metrics carries queue depth by state, oldest pending age, job results, duration and last success
- [x] docs carry example alert rules for failed, unknown, stuck and stalled normalization
- [x] GET /v1/stats returns a normalization object with counts, queue, oldest age and last failure
- [x] terva-lampi status prints the lake's normalization state
- [x] a storage sample is taken when the normalize queue drains after a batch

## Implementation plan

All three surfaces read one in-process snapshot.

- **Worker.** `runNormalize` returns its outcome: ok, failed, retried, or
  superseded (a newer ingest replaced the job). The worker loop times each
  job and records the outcome in `normalizeStats`: result counters, a
  duration histogram, the last success, and the last failure's session.
  `normalizeQueue.depth()` gives queued, running and retrying.
- **Catalog.** `NormalizeBacklog` returns the job table's row count and
  oldest `enqueued_at`, which covers jobs this process has not loaded.
  `NormalizationCounts` returns sessions by state.
- **`/metrics`.** Adds `lampi_normalize_pending_jobs`,
  `lampi_normalize_oldest_pending_age_seconds`,
  `lampi_normalize_jobs{state}`, `lampi_normalize_jobs_total{result}`,
  the `lampi_normalize_duration_seconds` histogram, and
  `lampi_normalize_last_success_timestamp_seconds`.
- **`/v1/stats`.** Gains an optional `normalization` object. Agents built
  before it ignore the field, and new agents print nothing extra against
  an old lake. `terva-lampi status` prints `lake_normalization`,
  `lake_normalize_jobs` and `lake_normalize_last_failure`.
- **Storage sample.** The queue reports when it goes idle after at least
  20 jobs, and serve's storage sampler takes a sample then. A normal
  upload normalizes one session at a time and never triggers it.
- **Docs.** The alert rules are in deploy/README.md; the /v1/stats shape
  is in docs/protocol.md.

## Notes

**agent:claude-code/e4a47e8c** at 2026-09-28T06:17:41Z

### Decision: a failure is reported without its message

`/v1/stats` reports the last failure's session and time but not its
message. docs/protocol.md already keeps error detail that can name a lake
path in the server log rather than in responses. A device token is held
by every capturing machine, and the message adds nothing an alert needs.
The dashboard does not show the message either, so the server log remains
the one place to read it.

### Rejected: counting results in the catalog

That would make the counters survive restarts, but every job would write
a row, and Prometheus handles counter resets already. The in-process
counters start at zero at each serve start; the docs say so.
