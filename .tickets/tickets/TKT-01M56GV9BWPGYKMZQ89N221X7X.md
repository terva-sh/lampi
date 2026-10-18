---
schema: 3
id: TKT-01M56GV9BWPGYKMZQ89N221X7X
title: "Parquet: bound serialization and row-group buffers"
type: task
status: in-progress
status_reason: null
priority: high
due_on: null
labels:
  - area/normalize
  - area/server
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
  commit: 8456f27e91f5cdfa4888a48146132effe461fc45
  session: null
  claimed_at: 2026-10-18T03:33:40Z
  expires_at: null
archive: null
created_at: 2026-10-18T03:28:22Z
updated_at: 2026-10-18T03:36:15Z
created_by:
  id: agent:codex/memory-e2fc465b
  name: ""
updated_by:
  id: agent:codex/memory-e2fc465b
  name: ""
extensions: {}
---

## Description

Write partitioned Parquet incrementally instead of building a whole-session map of serialized ParquetRow values. Bound batches and writer row-group buffers by bytes as well as row count, accounting for a single oversized event. Preserve partition ordering, complete event JSON, compression, file permissions, replacement and stale partition removal. Stage files privately and clean up failed writes. Keep input Event slices compatible for now so this reduction can land independently of the streaming normalizer. Avoid a whole-session second serialization, and measure RSS, allocation, throughput and output-size changes with the harness before and after.

## Acceptance criteria

- [x] Parquet output no longer retains serialized rows for the entire session; temporary writer memory is bounded by a documented batch/row-group budget plus the largest event.
- [x] Round-trip tests preserve full event JSON, interleaved date/harness ordering, compression, permissions, stale removal and cleanup after failure.
- [ ] Comparable 32 MiB and 128 MiB measurements record memory, CPU/time and output-size changes; project gates pass.

## Implementation plan

Group only Event indices by partition, keeping the existing Event slice interface. Process one partition writer at a time, serializing rows into byte/row-bounded batches and explicitly flushing byte/row-bounded row groups. Use a strings.Builder for event JSON to remove an extra full-string copy. Stage every partition privately and finish all serialization before renaming any output; clear batch references and remove temporary files on failures. Metadata indices and Parquet footer metadata still scale with event/row-group counts; this ticket bounds the heavy serialized text and column/page buffers, while full input streaming remains separate. Preserve compression, event JSON and partition order. Add round-trip tests for interleaved partitions, byte-limited groups and an oversized event, plus late serialization failure retaining old output. Compare repeated 32/128 MiB workloads and combined sync against the recorded tool baseline, including output size and CPU. Run package race tests and repository gates.

## Notes

**agent:codex/memory-e2fc465b** at 2026-10-18T03:36:14Z

Implemented one-writer-at-a-time partition staging with 128-row/1 MiB serialized batches and 2048-row/8 MiB row groups (plus a whole oversized event). Retain only event indices rather than all serialized rows, and use a string builder to remove the event JSON copy. Tests cover complete JSON and interleaved ordering, bounded groups, giant events, permissions/compression/stale removal and late serialization failure preserving old files and cleaning temps. Package tests and normalize/tool race tests pass. Three-run median projection RSS falls 182.6 -> 148.5 MiB at 32 MiB and 690.5 -> 591.2 MiB at 128 MiB; Parquet-stage allocation falls 534.3 -> 252.7 MiB at 128 MiB. Output grows 1.4%; retain the 8 MiB budget because it bounds text buffers for little storage loss. Total input events, partition indices and footer metadata remain outside this ticket's buffer bound. A combined sync sample did not improve peak RSS (529.4 -> 543.6 MiB), so document the limitation rather than generalizing the projection win. Full repository tests are running; vet and build pass. Alternatives rejected: processing all partitions with simultaneous writers multiplies buffers; rescanning every event for each partition repeats CPU; serializing all rows first retains the original problem. Full streaming remains the next separate ticket.
