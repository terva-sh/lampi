---
schema: 3
id: TKT-01M56HA9EQBBA9KF4Q9B8PSQ1R
title: "Agent: stream verified artifacts from file-backed snapshots"
type: task
status: ready
status_reason: null
priority: high
due_on: null
labels:
  - area/agent
  - area/redact
assignees: []
milestone: null
parent: TKT-01M56GHAXEJJXEE45GJ9C37NQG
origin: null
dependencies:
  - TKT-01M56GV9JT00CXN9RE3T6SNRGE
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-18T03:36:34Z
updated_at: 2026-10-18T03:36:34Z
created_by:
  id: agent:codex/memory-e2fc465b
  name: ""
updated_by:
  id: agent:codex/memory-e2fc465b
  name: ""
extensions: {}
---

## Description

Split the single-artifact streaming phase from TKT-01M56GV9JT00CXN9RE3T6SNRGE (Agent: bound upload batches and release transcript buffers) before implementation. Batching bounds how many sessions are prepared concurrently; it does not bound the largest session or remove a full-file read. These require different correctness arguments and measurements, and neither acceptance criterion should disappear into the other.

Use immutable private file-backed snapshots or verified chunks so digest checking, secret scanning, PUT retries and tail-prefix fallback see the same bytes. Hash and scan with bounded buffers, preserving the boundary/backtracking rules for long secret patterns and JSON escapes. Fall back safely when a source file changes; never upload unverified newer bytes from a reused path. Existing TKT-01M44DRPS (Cursor CLI: stream the export so large stores need not be capped) remains the export-specific work and should reuse the shared path where practical.

Record before/after agent allocation/RSS and small-append CPU. A tail slice retaining the full original byte buffer is insufficient; a lower GOMEMLIMIT without a working-set reduction is also insufficient.

## Acceptance criteria

- [ ] Large shared artifact transfers use bounded file-backed/chunk buffers rather than a full transcript allocation; document the largest-record/chunk memory contract.
- [ ] Preserve complete digest verification, same-size rewrite detection, redaction across boundaries, retries and full-file fallback when a tail is refused.
- [ ] Isolated large-artifact and boundary/rewrite/retry tests plus comparable profiles demonstrate the memory bound without changing capture permissions.
