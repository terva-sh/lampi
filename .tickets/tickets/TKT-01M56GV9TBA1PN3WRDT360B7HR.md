---
schema: 3
id: TKT-01M56GV9TBA1PN3WRDT360B7HR
title: "Normalize: stream JSONL projection through bounded buffers"
type: task
status: ready
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
updated_at: 2026-10-18T03:28:22Z
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
