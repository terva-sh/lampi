---
schema: 3
id: TKT-01M56M4CD83MDCQ14FGHB7MNBR
title: "Normalize: define durable append identity and checkpoint contract"
type: spike
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/normalize
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-18T04:25:46Z
updated_at: 2026-10-18T04:25:46Z
created_by:
  id: agent:codex/memory-e2fc465b
  name: ""
updated_by:
  id: agent:codex/memory-e2fc465b
  name: ""
extensions: {}
---

## Description

Follow-up to TKT-01M56GVA966MW31426MC7552PC (CPU: evaluate incremental projection and search prefix reuse); design rationale is in docs/incremental-projection.md. The bounded-memory release keeps full-session semantic behavior. Appends still regenerate random event IDs, ingestion timestamps and content references to the new full artifact digest. Reusing immutable prefixes therefore needs an explicit versioned identity/freshness contract before implementation.

Define durable per-artifact parser checkpoints and source-prefix proofs without trusting only filename/size/mtime. Specify all Claude/Codex/terva state, malformed-line counters, complete-record offsets, incomplete trailing records, metadata/options/normalizer-version invalidation and ordered sidecars. Decide how prior event IDs/ingestion timestamps and old immutable blob references survive append, compaction and CAS collection. Publish a checkpoint and event/Parquet generation only after validating current gen/head; restart must find either a complete generation or a durable full rebuild job.

After the semantic contract is approved, prototype a 64 KiB append to a 128 MiB session. Target at least 50% less normalize CPU than full projection over three fresh processes, without increasing the bounded-stream working set. Record verification/hash I/O separately; a valid fallback can remain O(input). No promise of O(append) total cost is valid until prefix proof and CAS lifetime are established. This follow-up is outside the completed memory epic's release scope.

## Acceptance criteria

- [ ] Versioned event identity, ingestion time and content-reference lifetime decisions are documented before implementation.
- [ ] Differential cases cover rewrites, truncate/regrow, partial lines, parser state, sidecar replacement and metadata/version invalidation.
- [ ] Crash/supersession tests prove checkpoint and output publication recover safely; prototype measures the 128 MiB append CPU target.
