---
schema: 3
id: TKT-01M56GV9JT00CXN9RE3T6SNRGE
title: "Agent: bound upload batches and release transcript buffers"
type: task
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area/agent
  - area/protocol
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
created_at: 2026-10-18T03:28:22Z
updated_at: 2026-10-18T03:46:03Z
created_by:
  id: agent:codex/memory-e2fc465b
  name: ""
updated_by:
  id: agent:codex/memory-e2fc465b
  name: ""
extensions: {}
---

## Description

Bound the number of sessions whose complete prepared bodies are retained during a sync. Prepare, scan, enqueue, send and ACK one admitted session at a time rather than accumulating all changed sessions before upload. Keep discovery/inventory metadata separate from raw bodies, and continue all other sessions when a session is refused, quarantined, unreadable or has no matching bay. Preserve digest/tail checks, full-file fallback, deduplication, retry/outbox durability and unchanged-file memoization.

This ticket bounds multi-session batch retention. The remaining full-artifact allocation is explicitly covered by TKT-01M56HA9EQBBA9KF4Q9B8PSQ1R (Agent: stream verified artifacts from file-backed snapshots), which depends on this step. Splitting before implementation keeps the two memory contracts independently reviewable; it does not claim one-session batching makes a large individual transcript bounded.

Use the synthetic workload runner and ordering tests to prove the next session is not scanned/prepared while the previous session's bodies are still pending ACK. Record combined-process measurement scope honestly until agent-only runtime diagnostics exist.

## Acceptance criteria

- [x] Preparing many changed sessions no longer holds all their raw bodies simultaneously, and completed session buffers can be released before the next preparation.
- [x] Isolated multi-session, unchanged, append, rewrite, retry and refusal tests pass, with measured agent memory growth and allocation changes.
- [x] Preserve cross-session blob deduplication and retry/outbox behavior, including mixed refusals, unreadable files, tail-prefix fallback and no-bay continuation.

## Implementation plan

Refactor the shared preparation loop to deliver one enqueued prepared session to a consumer and accumulate results/refusals without retaining prior bodies. Keep the prepare() collector for existing local preparation tests, implemented over the same loop. Move upload/split/fallback/post/ACK into a per-session send helper. Carry only small digest/chunk metadata between sessions to preserve dedup and avoid redundant checks, clearing any temporary body aliases before the next preparation. Protect partial result accounting on network failure, unchanged/refused behavior and complete inventory reporting. Add HTTP ordering and mixed-outcome tests, run existing upload/accept/CLI tests and race checks, and compare isolated sync workloads to the Parquet-only baseline. Do not claim per-artifact streaming is complete.

## Notes

**agent:codex/memory-e2fc465b** at 2026-10-18T03:36:35Z

Before coding, split full-artifact file-backed streaming into TKT-01M56HA9EQBBA9KF4Q9B8PSQ1R (Agent: stream verified artifacts from file-backed snapshots). The original second criterion is now the new ticket's acceptance criteria and remains unfulfilled there. This ticket implements the independently useful per-session batch lifetime reduction, retaining full-artifact scanning semantics until the dependency's successor replaces them.

**agent:codex/memory-e2fc465b** at 2026-10-18T03:40:50Z

The sequential preparation/send implementation and targeted upload/accept/tool tests now pass. New tests cover ACK-before-next-scan ordering, durable pending state after a failed manifest, cross-session shared sidecar deduplication with/without chunking, and no-bay continuation/retry. Corrected writeClaudeCorpus to JSON-escape generated text; its previous embedded quotes produced malformed benchmark JSON and missing CWDs. Fixed-size chunks now share the already immutable snapshot, as splitAt already does, avoiding an extra whole-body copy while the snapshot is necessarily alive. Adding a separate-process synthetic lake mode to lampi-perf so this ticket can report agent-only memory rather than attributing combined normalize/agent RSS to the agent.

**agent:codex/memory-e2fc465b** at 2026-10-18T03:43:53Z

Implementation is committed at c9e2404; the separate-process agent measurement mode is f6e52a6. Full go test -trimpath=false ./..., upload/accept/tool race tests, go vet ./..., formatting and the application build passed. A subsequent tool-only test also passes after adding explicit RUSAGE_SELF CPU accounting to avoid including waited-for lake descendants. Alternating old/new agent measurements are running using identical updated tools in both builds, with production upload code before c9e2404 as the control. No live services or config were changed. Batching intentionally retains a full immutable snapshot of the current session; no per-artifact bounded-memory claim is made.

**agent:codex/memory-e2fc465b** at 2026-10-18T03:46:03Z

Repeated alternating comparisons (three fresh runs/build/workload) are complete. Both control and new builds use the identical f6e52a6 runner; control production is before c9e2404 with Parquet improvements present, and new production is c9e2404. The three 32 MiB session workload includes initial/unchanged/two 64 KiB append passes. Agent-only median RSS fell 282.6→154.8 MiB (ranges 282.4–284.4 and 153.5–156.9); sampled peak heap 230.6→129.5 MiB; stage allocation 493.6→397.1 MiB. Combined agent/lake RSS fell 554.0→432.6 MiB (ranges 503.5–616.0 and 427.5–442.7). Combined allocation/CPU ranges overlap, and agent CPU median rose 1.71→1.87 s with overlapping ranges, so no CPU speedup claim. Output counters match for all passes and lake storage remains about 70.94 MiB. Single-run agent scaling checks at 1 and 6 sessions recorded 152.6→156.3 MiB and 525.3→175.3 MiB respectively; not strict bounds.

Batching and fixed-chunk slice sharing are measured together. Both avoidable retention and a full-body chunk copy were removed. Appends still allocate roughly a complete current transcript per session: this is why file-backed artifact streaming remains a separate open ticket. No changes to gates, capture permissions or live processes. Detailed methods, ranges, CPU/output tradeoffs and limitations are in docs/performance.md. Disposable JSON reports and binaries are under /home/sothr/.cache/agent-scratch/lampi/implementation-gACllt (batch-agent-*, batch-sync-*, batch-agent-scaling-*); the durable results are in source control.

## Summary

Landed c9e2404 session-at-a-time prepare/send/ACK with digest-only cross-session state, shared immutable fixed-size chunks, retry/no-bay continuation and dedicated regression tests. f6e52a6 adds a separate-process synthetic lake mode so agent memory/allocation/CPU are measurable independently. Repeated three-session agent RSS fell about 45% (282.6→154.8 MiB), with about 20% lower stage allocation; combined RSS fell about 22% (554.0→432.6 MiB) for this case. CPU did not show a stable improvement. Full repository tests, vet, build, formatting and targeted race tests pass. Full-artifact memory remains in the separately tracked file-backed snapshot ticket; live services were not changed.
