---
schema: 3
id: TKT-01M56GVANEY48KGD9JZSREWKA8
title: "Perf: validate memory budgets and document deployment choices"
type: task
status: ready
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
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-18T03:28:23Z
updated_at: 2026-10-18T03:28:23Z
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
