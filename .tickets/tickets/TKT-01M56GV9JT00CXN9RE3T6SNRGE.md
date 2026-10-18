---
schema: 3
id: TKT-01M56GV9JT00CXN9RE3T6SNRGE
title: "Agent: bound upload batches and release transcript buffers"
type: task
status: in-progress
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
claim:
  actor: agent:codex/memory-e2fc465b
  branch: t3code/explore-lampi-memory-footprint
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-e2fc465b
  commit: 2616854df71fbde82afe686a14e68a12d623c9df
  session: null
  claimed_at: 2026-10-18T03:36:36Z
  expires_at: null
archive: null
created_at: 2026-10-18T03:28:22Z
updated_at: 2026-10-18T03:40:50Z
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

- [ ] Preparing many changed sessions no longer holds all their raw bodies simultaneously, and completed session buffers can be released before the next preparation.
- [ ] Isolated multi-session, unchanged, append, rewrite, retry and refusal tests pass, with measured agent memory growth and allocation changes.
- [ ] Preserve cross-session blob deduplication and retry/outbox behavior, including mixed refusals, unreadable files, tail-prefix fallback and no-bay continuation.

## Implementation plan

Refactor the shared preparation loop to deliver one enqueued prepared session to a consumer and accumulate results/refusals without retaining prior bodies. Keep the prepare() collector for existing local preparation tests, implemented over the same loop. Move upload/split/fallback/post/ACK into a per-session send helper. Carry only small digest/chunk metadata between sessions to preserve dedup and avoid redundant checks, clearing any temporary body aliases before the next preparation. Protect partial result accounting on network failure, unchanged/refused behavior and complete inventory reporting. Add HTTP ordering and mixed-outcome tests, run existing upload/accept/CLI tests and race checks, and compare isolated sync workloads to the Parquet-only baseline. Do not claim per-artifact streaming is complete.

## Notes

**agent:codex/memory-e2fc465b** at 2026-10-18T03:36:35Z

Before coding, split full-artifact file-backed streaming into TKT-01M56HA9EQBBA9KF4Q9B8PSQ1R (Agent: stream verified artifacts from file-backed snapshots). The original second criterion is now the new ticket's acceptance criteria and remains unfulfilled there. This ticket implements the independently useful per-session batch lifetime reduction, retaining full-artifact scanning semantics until the dependency's successor replaces them.

**agent:codex/memory-e2fc465b** at 2026-10-18T03:40:50Z

The sequential preparation/send implementation and targeted upload/accept/tool tests now pass. New tests cover ACK-before-next-scan ordering, durable pending state after a failed manifest, cross-session shared sidecar deduplication with/without chunking, and no-bay continuation/retry. Corrected writeClaudeCorpus to JSON-escape generated text; its previous embedded quotes produced malformed benchmark JSON and missing CWDs. Fixed-size chunks now share the already immutable snapshot, as splitAt already does, avoiding an extra whole-body copy while the snapshot is necessarily alive. Adding a separate-process synthetic lake mode to lampi-perf so this ticket can report agent-only memory rather than attributing combined normalize/agent RSS to the agent.
