---
schema: 3
id: TKT-01M56GV9TBA1PN3WRDT360B7HR
title: "Normalize: stream JSONL projection through bounded buffers"
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
  - TKT-01M56GV9BWPGYKMZQ89N221X7X
blocks_on: none
references: []
claim:
  actor: agent:codex/memory-e2fc465b
  branch: t3code/explore-lampi-memory-footprint
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-e2fc465b
  commit: 10ee962713f34bc3058d155b18f481451792594c
  session: null
  claimed_at: 2026-10-18T04:12:17Z
  expires_at: null
archive: null
created_at: 2026-10-18T03:28:22Z
updated_at: 2026-10-18T04:12:18Z
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

- [ ] JSONL projection can consume CAS readers and emit staged events/Parquet with a documented bounded working set independent of full transcript length.
- [ ] Existing normalization fixtures and new streamed-versus-compatibility, malformed/cancelled, sidecar and superseded-generation cases preserve output semantics.
- [ ] Large and multi-worker synthetic profiles demonstrate reduced working memory; failure never publishes a partial generation.

## Implementation plan

Read the existing Claude/Codex/terva per-line parsers, indexed zstd events, bounded Parquet writer and generation publication path. Add reader-to-sink methods reusing exact parser state and line/offset/error semantics; retain the slice entry points as compatibility references. Memory is proportional to the largest input record/event plus bounded output buffers and partition/frame metadata, not transcript length. Stage compressed events, spool partition rows to private disk, and serialize one bounded Parquet partition at a time. Open logical CAS readers rather than materializing entire blobs. Finish all staged outputs before checking generation and publishing, clean cancelled/superseded output, and preserve existing error/removal behavior. Other harnesses keep the compatibility path. Validate semantic fixture parity, malformed/cancelled/read failures, sidecars and supersession, then compare 128 MiB and overlapping sync workloads against the committed pre-streaming build without touching live state. Do not change catalog schema or normalizer semantics versions.
