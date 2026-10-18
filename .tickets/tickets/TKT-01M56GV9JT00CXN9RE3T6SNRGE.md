---
schema: 3
id: TKT-01M56GV9JT00CXN9RE3T6SNRGE
title: "Agent: bound upload batches and release transcript buffers"
type: task
status: ready
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

The shared upload pipeline prepares every changed session and retains its full transcript until the sync ends. First introduce a bounded per-session or byte-budgeted preparation/send/ACK pipeline, preserving discovery/inventory and all refusal reporting. Then replace full artifact buffers with immutable file-backed snapshots or bounded chunks as measurement warrants. Keep digest checks, redaction boundaries, tail fallback, chunk deduplication, retry/outbox durability and same-size rewrite detection. Do not move an allow check later or broaden uploads. Existing Cursor CLI streaming work is separate and may later reuse this shared path. A tail slice into an otherwise unused full allocation does not meet the final bounded-buffer goal.

## Acceptance criteria

- [ ] Preparing many changed sessions no longer holds all their raw bodies simultaneously, and completed session buffers can be released before the next preparation.
- [ ] Large artifact transfers use bounded file-backed/chunk buffers with verified digests, redaction boundary handling and full-file fallback semantics.
- [ ] Isolated multi-session, unchanged, append, rewrite, retry and refusal tests pass, with measured agent memory growth and allocation changes.
