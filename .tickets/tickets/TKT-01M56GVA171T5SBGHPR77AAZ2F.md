---
schema: 3
id: TKT-01M56GVA171T5SBGHPR77AAZ2F
title: "Normalize: coalesce jobs and budget concurrent large sessions"
type: task
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
  - area/normalize
assignees: []
milestone: null
parent: TKT-01M56GHAXEJJXEE45GJ9C37NQG
origin: null
dependencies:
  - TKT-01M56GV8XHXWHZ2N41XFP0H8H0
blocks_on: none
references: []
claim:
  actor: agent:codex/memory-e2fc465b
  branch: t3code/explore-lampi-memory-footprint
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-e2fc465b
  commit: 1a794390b3a902ab07437d9f75512f5ec99c5df5
  session: null
  claimed_at: 2026-10-18T04:15:23Z
  expires_at: null
archive: null
created_at: 2026-10-18T03:28:23Z
updated_at: 2026-10-18T04:15:24Z
created_by:
  id: agent:codex/memory-e2fc465b
  name: ""
updated_by:
  id: agent:codex/memory-e2fc465b
  name: ""
extensions: {}
---

## Description

Prevent avoidable overlap of expensive projections for the same session and limit combined working memory across sessions. Measure byte-weighted admission versus a lower worker count, without assuming GOMEMLIMIT is a hard bound. Coalesce pending generations but preserve durable catalog jobs, retries, session fairness, shutdown drain and latest-generation checks. Never drop a job solely because its queue item became obsolete. Record queue latency and throughput alongside RSS; optimize for bounded work, not a misleading empty queue.

## Acceptance criteria

- [ ] Pending obsolete work is coalesced and simultaneous generations of one session do not duplicate expensive projection.
- [ ] A documented admission budget limits overlapping large sessions while preserving small-session fairness, durable retries and shutdown behavior.
- [ ] Concurrency, replacement and restart tests plus isolated burst measurements record peak memory, throughput and queue latency.

## Implementation plan

Coalesce queued work by session while preserving the catalog's latest durable generation and in-flight/retry accounting. Serialize expensive projection per session using queue admission rather than holding the publication lock across manifest ingest or occupying a worker waiting on a session mutex. Add explicit per-job logical-input weight from catalog current artifacts; admit at most the configured byte budget (oversized jobs run alone), with FIFO reservation so small jobs cannot permanently starve a large job. Defaults retain two workers and a moderate logical-input budget; expose explicit serve flags through validated open options. Preserve delayed retries and restart loading; superseded retries must never replace a newer queued generation. Record admitted bytes, coalesced count and queue wait alongside existing duration/depth metrics. Validate replacement, simultaneous generations, budget/oversize, retry, shutdown/restart and a measured isolated append burst, comparing worker/admission settings before recommendations.
