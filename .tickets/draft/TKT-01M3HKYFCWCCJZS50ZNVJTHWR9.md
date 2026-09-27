---
schema: 3
id: TKT-01M3HKYFCWCCJZS50ZNVJTHWR9
title: Operator re-normalization for stale and failed sessions
type: task
status: draft
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
updated_at: 2026-09-27T14:22:47Z
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

- [ ] serve normalize enqueues stale, failed or named sessions and reports the count
- [ ] A running lake processes the new jobs without a restart
- [ ] Dashboard overview unknown and failed counts fall as jobs finish
