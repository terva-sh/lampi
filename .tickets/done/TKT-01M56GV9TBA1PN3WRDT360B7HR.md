---
schema: 3
id: TKT-01M56GV9TBA1PN3WRDT360B7HR
title: "Normalize: stream JSONL projection through bounded buffers"
type: task
status: done
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
  - TKT-01M56GV9BWPGYKMZQ89N221X7X
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-18T03:28:22Z
updated_at: 2026-10-18T04:31:29Z
created_by:
  id: agent:codex/memory-e2fc465b
  name: ""
updated_by:
  id: agent:codex/memory-e2fc465b
  name: ""
extensions: {}
---

## Description

Introduce reader-to-event-sink projection for JSONL harnesses, starting with Claude/Codex/terva while keeping compatibility for other harnesses. Stream CAS data and derived events into private staged output rather than requiring raw []byte and a full []Event at the same time. Preserve all event semantics, raw byte offsets/content refs, parser state, unknown fields, malformed-input behavior, sidecars, context cancellation and complete-generation publication. Use the bounded Parquet sink from its dependency. A single large record needs an explicit limit or largest-record memory contract. Continue to support export and existing normalizer entry points. Do not bump persisted semantics or requeue all sessions solely for a storage implementation change.

## Acceptance criteria

- [x] JSONL projection can consume CAS readers and emit staged events/Parquet with a documented bounded working set independent of full transcript length.
- [x] Existing normalization fixtures and new streamed-versus-compatibility, malformed/cancelled, sidecar and superseded-generation cases preserve output semantics.
- [x] Large and multi-worker synthetic profiles demonstrate reduced working memory; failure never publishes a partial generation.

## Implementation plan

Read the existing Claude/Codex/terva per-line parsers, indexed zstd events, bounded Parquet writer and generation publication path. Add reader-to-sink methods reusing exact parser state and line/offset/error semantics; retain the slice entry points as compatibility references. Memory is proportional to the largest input record/event plus bounded output buffers and partition/frame metadata, not transcript length. Stage compressed events, spool partition rows to private disk, and serialize one bounded Parquet partition at a time. Open logical CAS readers rather than materializing entire blobs. Finish all staged outputs before checking generation and publishing, clean cancelled/superseded output, and preserve existing error/removal behavior. Other harnesses keep the compatibility path. Validate semantic fixture parity, malformed/cancelled/read failures, sidecars and supersession, then compare 128 MiB and overlapping sync workloads against the committed pre-streaming build without touching live state. Do not change catalog schema or normalizer semantics versions.

## Notes

**agent:codex/memory-e2fc465b** at 2026-10-18T04:21:40Z

The initial JSON row spool reduced RSS but increased CPU/allocation; replaced it with a private length-prefixed string spool to avoid escaping and reparsing EventJSON. The stage retains one event/row plus bounded frame/Parquet buffers, with metadata proportional to partitions/frames and input memory proportional to the largest record. Added producer-panic cleanup and abandoned-stage recovery before worker startup under the exclusive CLI lake lock. Refined combined agent/lake control comparisons are running; prototype figures are not final performance claims.

**agent:codex/memory-e2fc465b** at 2026-10-18T04:31:28Z

Completed reader-driven Claude/Codex/terva JSONL projection into private staged indexed-zstd events and per-partition bounded Parquet output. Compatibility slice APIs remain references; other harnesses keep their slice projectors. Fixture/differential tests preserve all fields except random event IDs, malformed/error sidecars/CRLF/final partial lines, reader/sink errors, cancellation and latest-generation supersession. Stage tests cover failures before publication, rename failure, panic and abandoned-stage cleanup. Truncated private spool rows are rejected. Large and two-worker profiles parsed successfully; three alternating fresh-process comparisons against production 10ee962 reduced combined RSS medians 418.2→235.2 MiB (3x32 MiB) and 665.1→174.2 MiB (1x128 MiB), at roughly unchanged CPU but increased allocation/GC. Production implementation is 635dea4 plus 50fb38f error handling. docs/performance.md documents largest-record/metadata limits, compatibility paths, per-file publication, disk/cache costs, exact controls and limitations. Full release gates are tracked by final validation; no deployment occurred.

## Summary

Streamed CAS JSONL through private staged events and bounded Parquet, preserving semantics and generation checks. Tests and large/multiworker profiles validate lower working memory; report records disk/allocation tradeoffs and remaining largest-record/compatibility limits.
