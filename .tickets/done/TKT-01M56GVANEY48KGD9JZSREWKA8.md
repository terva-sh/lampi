---
schema: 3
id: TKT-01M56GVANEY48KGD9JZSREWKA8
title: "Perf: validate memory budgets and document deployment choices"
type: task
status: done
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
claim: null
archive: null
created_at: 2026-10-18T03:28:23Z
updated_at: 2026-10-18T04:40:23Z
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

- [x] Repeated before/after measurements cover both processes and distinguish RSS, heap, allocation rate, file cache and CPU/queue latency.
- [x] Memory-limit/debounce/admission recommendations follow measured working-set and freshness tradeoffs and include rollback guidance.
- [x] Repository gates pass and a reviewable performance report names build revisions and limitations; live deployment remains a separate authorized operation.

## Implementation plan

Finish repeated alternating combined 3x32 MiB and 1x128 MiB control/streaming comparisons; preserve prior isolated agent results and repeat current agent runs. Measure three fresh-process append bursts across worker count/input budget and default/moderate GOMEMLIMIT, reporting coalescing, outcomes, queue waits, stage allocation/heap/RSS and CPU. Compare debounce by identical burst input with one final sync versus syncing every append, explicitly exposing added freshness delay; do not alter defaults from a synthetic result. Keep profiling separate; capture large/multiworker profiles and real search/streaming component measurements. Record revisions, runtime, disk/I/O/cache limits and rollback choices in docs/performance.md, write release notes (no schema/semantic migration), run full race/vet/format/build and release-target validation. Leave publication/deployment separate.

## Notes

**agent:codex/memory-e2fc465b** at 2026-10-18T04:40:22Z

Completed repeated measurements and release rehearsal. docs/performance.md and docs/performance-results.json preserve three-process combined before/after runs (3x32 MiB and 1x128 MiB), prior/current agent-only results, 15 burst settings, independent streaming/search costs, separate profiles, six file-cache runs, input/transfer/outcome/wait counters, binary hashes, revisions and limitations. Combined RSS medians fell 418.2→235.2 MiB and 665.1→174.2 MiB, with increased allocation/GC and roughly unchanged CPU. One worker reduced burst CPU about28% at longer queue/wall time. Moderate256 MiB GOMEMLIMIT and modeled source debounce remain optional tradeoffs; no live tuning. Post-exit fincore found ~167.45 MiB cached private pages under either runtime limit; all six retained catalogs had3/3 latest ready sessions and0 jobs.

At clean revision45fea68, formatting/whitespace/vet, full repository race tests, application/tool builds, GoReleaser configuration, five-target snapshot archives/checksums/notices and linux/amd64 archive smoke tests passed. Buildah built both linux/amd64 and linux/arm64 Dockerfile targets; amd64 container version/help smoke tests passed. Snapshot artifacts remain local; version0.8.1-next was not selected or tagged as a release. docs/release-notes-memory.md records schema/requeue compatibility and rollback. git ticket check reports zero errors and the one pre-existing unrelated long-title warning. Remote review/merge/tagging/publication and deployment remain separate; no live service/config/capture changes occurred.

## Summary

Recorded repeated agent/lake memory, CPU, cache and queue tradeoffs with deployment/rollback guidance. Full race/vet/format/build, five release archives/checksums and both container builds pass; release notes prepared with no schema/requeue change. Publication/deployment remain separate.
