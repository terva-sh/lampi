---
schema: 3
id: TKT-01M3HKYFCWCCJZS50ZNVJTHWR9
title: Operator re-normalization for stale and failed sessions
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/normalize
  - area/ops
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-27T14:22:47Z
updated_at: 2026-09-27T21:48:26Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/e4a47e8c
  name: ""
extensions: {}
---

## Description

Sessions ingested before generation tracking landed (TKT-01M3F2K1R Catalog: track published normalization generations explicitly) have no published generation, so the dashboard counts them as `unknown`. Normalization is only enqueued when an upload changes a session, and a restart reloads only rows already in `normalize_jobs`, so a session that never grows again stays `unknown` forever. `failed` sessions likewise have no retry path after the five attempts run out.

### Approach

Add `serve normalize` with `--stale` (every session whose state is `unknown`), `--failed` (clear `normalize_error` and requeue), and `--session UID`. It writes rows through `EnqueueNormalize`, which bumps the generation, so a running lake picks them up. Because the in-process queue is loaded only at start, either signal the running lake (SIGHUP already reloads profiles; add a queue reload) or run it offline before a start. Report the count enqueued. Progress is visible on the dashboard overview's normalization counts.

Alternatives: enqueue every `unknown` session automatically at start. Rejected as the only mechanism because a large lake would re-project everything on each upgrade without the operator choosing to, and a permanently failing session would retry every start.

## Acceptance criteria

- [x] serve normalize enqueues stale, failed or named sessions and reports the count
- [x] A running lake processes the new jobs without a restart
- [x] Dashboard overview unknown and failed counts fall as jobs finish

## Notes

**agent:claude-code/e4a47e8c** at 2026-09-27T21:48:26Z

### Implementation

- **`catalog.SessionsInNormalizationState`** lists sessions by the same CASE expression the dashboard counts with.
- **`terva-lampi serve normalize --stale|--failed|--session UID [--dry-run]`** enqueues each chosen session through `EnqueueNormalize`, which bumps the generation and upserts the job, and prints each UID with its prior state. It runs while serve runs.
- **Existing behaviour covers failed sessions:** a requeued failed session reads as pending, a success clears `normalize_error` (`MarkPublished`), and a new failure records it again.
- **Running lake:** SIGHUP now also calls `Server.ReloadNormalizeJobs`, which queues every `normalize_jobs` row the process does not hold. The in-memory queue counts jobs by (uid, gen) across queued, retry-timer and running, so a reload never duplicates. A job whose attempts ran out is held by nothing, so SIGHUP retries it the way a restart does.
- **The example serve unit** gains `ExecReload=/bin/kill -HUP $MAINPID`. brokkr's installed unit does not have it, so the messages say `systemctl kill -s HUP`.
- **Transcript page messages** now name the command for unknown, failed and missing-file sessions.

### Rejected

Queuing every unknown session automatically at start. A large lake would re-project everything on each upgrade without the operator choosing to (as the ticket already said). A periodic sweep of `normalize_jobs` was also rejected: it adds background polling, and it would retry exhausted jobs every sweep.

### Evidence

- Tests:
  - A job written by a second catalog handle runs after `ReloadNormalizeJobs` (race).
  - A second reload queues 0.
  - Queue dedupe covers queued, running, retry and after-shutdown jobs.
  - The CLI queues stale and failed but not ready, `--dry-run` writes nothing, and no selector is refused.
- End to end with the real binary: sync one session (ready), clear `published_gen` to mimic a pre-tracking row (unknown), run `serve normalize --stale` while serve runs (pending), SIGHUP (ready). The log said "started 1 normalize jobs".

## Summary

Added terva-lampi serve normalize --stale|--failed|--session UID [--dry-run]. It queues sessions the dashboard counts as unknown or failed, or named ones, while serve runs. A running serve starts the jobs on SIGHUP through a de-duplicating reload of normalize_jobs, and any serve starts them at startup. Progress shows in the dashboard overview's normalization counts. Verified end to end with the real binary.
