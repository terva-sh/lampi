---
schema: 3
id: TKT-01M56GVANEY48KGD9JZSREWKA8
title: "Perf: validate memory budgets and document deployment choices"
type: task
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area/ops
  - area/docs
assignees: []
milestone: null
parent: TKT-01M56GHAXEJJXEE45GJ9C37NQG
origin: null
dependencies:
  - TKT-01M56GV94E755W96818JGW691M
  - TKT-01M56GV9BWPGYKMZQ89N221X7X
  - TKT-01M56GV9JT00CXN9RE3T6SNRGE
  - TKT-01M56GV9TBA1PN3WRDT360B7HR
  - TKT-01M56GVA171T5SBGHPR77AAZ2F
  - TKT-01M56HA9EQBBA9KF4Q9B8PSQ1R
blocks_on: none
references: []
claim:
  actor: agent:codex/memory-e2fc465b
  branch: t3code/explore-lampi-memory-footprint
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-e2fc465b
  commit: 1be8f0bf9367dea5ea1937f2efb6623a6a8b99ce
  session: null
  claimed_at: 2026-10-18T04:24:16Z
  expires_at: null
archive: null
created_at: 2026-10-18T03:28:23Z
updated_at: 2026-10-18T04:24:17Z
created_by:
  id: agent:codex/memory-e2fc465b
  name: ""
updated_by:
  id: agent:codex/memory-e2fc465b
  name: ""
extensions: {}
---

## Description

Use the isolated runner to compare baseline and improved agent/lake builds under representative large transcripts, concurrent sessions, unchanged scans and small append bursts. Compare default and moderate GOMEMLIMIT, debounce and admission settings, recording memory, CPU, disk output size, throughput and freshness. Report medians/ranges over multiple processes; separate profile overhead and warm/cold caches. Define practical budgets from the measured working set rather than forcing a low cap. Document optional deployment values and rollback checks. Do not install a build, restart a live service, change capture policy or edit live configuration as part of benchmark validation.

## Acceptance criteria

- [ ] Repeated before/after measurements cover both processes and distinguish RSS, heap, allocation rate, file cache and CPU/queue latency.
- [ ] Memory-limit/debounce/admission recommendations follow measured working-set and freshness tradeoffs and include rollback guidance.
- [ ] Repository gates pass and a reviewable performance report names build revisions and limitations; live deployment remains a separate authorized operation.

## Implementation plan

Finish repeated alternating combined 3x32 MiB and 1x128 MiB control/streaming comparisons; preserve prior isolated agent results and repeat current agent runs. Measure three fresh-process append bursts across worker count/input budget and default/moderate GOMEMLIMIT, reporting coalescing, outcomes, queue waits, stage allocation/heap/RSS and CPU. Compare debounce by identical burst input with one final sync versus syncing every append, explicitly exposing added freshness delay; do not alter defaults from a synthetic result. Keep profiling separate; capture large/multiworker profiles and real search/streaming component measurements. Record revisions, runtime, disk/I/O/cache limits and rollback choices in docs/performance.md, write release notes (no schema/semantic migration), run full race/vet/format/build and release-target validation. Leave publication/deployment separate.
