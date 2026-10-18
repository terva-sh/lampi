---
schema: 3
id: TKT-01M56GVA171T5SBGHPR77AAZ2F
title: "Normalize: coalesce jobs and budget concurrent large sessions"
type: task
status: done
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
claim: null
archive: null
created_at: 2026-10-18T03:28:23Z
updated_at: 2026-10-18T04:31:30Z
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

- [x] Pending obsolete work is coalesced and simultaneous generations of one session do not duplicate expensive projection.
- [x] A documented admission budget limits overlapping large sessions while preserving small-session fairness, durable retries and shutdown behavior.
- [x] Concurrency, replacement and restart tests plus isolated burst measurements record peak memory, throughput and queue latency.

## Implementation plan

Coalesce queued work by session while preserving the catalog's latest durable generation and in-flight/retry accounting. Serialize expensive projection per session using queue admission rather than holding the publication lock across manifest ingest or occupying a worker waiting on a session mutex. Add explicit per-job logical-input weight from catalog current artifacts; admit at most the configured byte budget (oversized jobs run alone), with FIFO reservation so small jobs cannot permanently starve a large job. Defaults retain two workers and a moderate logical-input budget; expose explicit serve flags through validated open options. Preserve delayed retries and restart loading; superseded retries must never replace a newer queued generation. Record admitted bytes, coalesced count and queue wait alongside existing duration/depth metrics. Validate replacement, simultaneous generations, budget/oversize, retry, shutdown/restart and a measured isolated append burst, comparing worker/admission settings before recommendations.

## Notes

**agent:codex/memory-e2fc465b** at 2026-10-18T04:21:54Z

Implemented pending-generation coalescing at the original queue position, per-session running admission, conservative logical-input weights from current catalog artifacts, configurable workers and a 256 MiB default input budget. Oversized jobs run alone; at most eight smaller jobs bypass a capacity-blocked session before reserving capacity. Held/retry metadata is released when idle; obsolete retry timers cannot replace newer work. Added admission/queue-wait metrics and synthetic undrained append bursts. Targeted API/catalog/normalizer/runner tests pass including budget fairness, same-session serialization, latest generation and isolated combined/separate-process bursts. Existing retry/restart/reload tests remain part of the gate; measurement and documentation remain before closure.

**agent:codex/memory-e2fc465b** at 2026-10-18T04:31:29Z

Coalescing, same-session serialization, byte-weighted fairness/oversize admission, delayed retries and restart loading are implemented and tested. Strengthened restart coverage proves repeated durable enqueues reload only the newest generation with its correct logical weight and run an oversized job alone. Metrics and documented serve flags expose admission and waits. Three-process 3x16 MiB / eight-append bursts: default two workers admitted24/coalesced3 jobs, median RSS318.5 MiB, CPU23.44s, stage wall9.13s, mean/max queue wait0.42/0.98s. One worker admitted15/coalesced12, RSS248.3 MiB, CPU16.76s, wall10.20s, wait1.02/1.68s. A32 MiB budget gives similar serialization. All15 worker/budget/memory-limit/debounce experiment processes had zero failed or retried jobs. docs/performance.md records full comparison, fairness, retry/shutdown semantics and the fact that logical admission is not an RSS cap. Optional low-resource choices trade freshness for memory/CPU; defaults and live configuration are unchanged.

## Summary

Coalesced pending work, serialized sessions and added fair logical-input admission with durable retries/restart tests and queue metrics. Repeated bursts quantify worker/budget memory and CPU savings against queue/freshness cost; no live tuning.
