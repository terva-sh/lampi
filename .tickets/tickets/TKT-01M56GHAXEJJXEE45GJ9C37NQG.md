---
schema: 3
id: TKT-01M56GHAXEJJXEE45GJ9C37NQG
title: Bound memory and repeated work for growing transcripts
type: epic
status: ready
status_reason: null
priority: high
due_on: null
labels:
  - area/agent
  - area/server
  - area/normalize
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: children
references: []
claim: null
archive: null
created_at: 2026-10-18T03:22:56Z
updated_at: 2026-10-18T03:28:24Z
created_by:
  id: agent:codex/memory-e2fc465b
  name: ""
updated_by:
  id: agent:codex/memory-e2fc465b
  name: ""
extensions: {}
---

## Description

Investigating the live workstation on 2026-10-17 found multi-GiB transient allocations in the agent and lake. The report and proposed order of work are in docs/memory-investigation.md.

The lake process reached 4.32 GiB RSS since startup, and the agent 2.37 GiB. A 15-second observation saw the lake fall from 2.76 GiB to 176 MiB without intervention. A separate 45-second observation consumed 67.5 server CPU-seconds while only 143 KiB of blob uploads arrived and five normalize jobs completed. Growing Claude and Codex transcript files reached about 352 MiB and 117 MiB respectively.

Source inspection finds full artifact reads and batch retention in upload preparation, full CAS reads and event slices in projection, and a second serialized event representation in Parquet export. Tail uploads and tail secret scans already exist; they do not prevent full-file reads and hashing or full-session lake projection.

An isolated 128 MiB synthetic Claude transcript allocated about 1.4 GiB across normalization and export, with sampled peak RSS 598.5 MiB. A 512 MiB Go soft memory limit reduced sampled peak RSS to 511.9 MiB at similar elapsed time in one run. A 256 MiB limit increased time from about 2.25 seconds to 5.34 seconds, triggered 443 collections before completion, and still exceeded the soft limit. These are synthetic results, not attribution of the live heap.

Prefer bounded processing and removal of repeated work over a low unmeasured memory limit. Measure before changing compression levels or disabling derived stores. Tail-only normalization needs explicit handling of parser state, rewrites, content references and generation atomicity. Existing TKT-01M44DRPS (Cursor CLI: stream the export so large stores need not be capped) covers the Cursor export case; this ticket covers the shared upload and lake pipeline.

The live services, configuration, tokens and capture permissions were left unchanged. This is follow-up implementation work filed in draft, not a claim that the optimizations have landed.

## Acceptance criteria

- [ ] Record comparable RSS, live heap, allocation and CPU profiles for agent sync and lake projection on representative growing transcripts.
- [ ] Bound upload preparation memory by a limited batch or file-backed chunks, preserving digest checks, redaction boundaries, retries and rewrite detection.
- [ ] Stream lake normalization and Parquet output with bounded buffers while preserving event semantics and generation publication safety.
- [ ] Measure incremental projection or batching choices for small appends; document freshness and CPU tradeoffs without changing capture permissions.

## Notes

**agent:codex/memory-e2fc465b** at 2026-10-18T03:28:24Z

The user authorized an epic, child tickets, promotion and autonomous implementation on 2026-10-17. Converted the existing investigation follow-up to the epic and promoted its eight scoped children. Dependencies put reproducible tools before reductions and measurement before runtime tuning. Start with the workload runner, then bounded Parquet serialization; continue through shared agent batching and streamed projection. Retain the investigation as baseline evidence. Do not deploy or change live configuration during this work. Child acceptance criteria are intentionally unchecked until implementation and measurement earn them.
