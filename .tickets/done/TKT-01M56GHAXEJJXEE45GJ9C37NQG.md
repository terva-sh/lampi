---
schema: 3
id: TKT-01M56GHAXEJJXEE45GJ9C37NQG
title: Bound memory and repeated work for growing transcripts
type: epic
status: done
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
updated_at: 2026-10-18T04:40:25Z
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

The live services, configuration, tokens and capture permissions were left unchanged. The user authorized promotion and autonomous implementation of this epic and its nine child tickets. Current implementation and measurement results are in docs/performance.md and the child tickets; this initial investigation remains provenance rather than a claim about deployed improvements.

## Acceptance criteria

- [x] Record comparable RSS, live heap, allocation and CPU profiles for agent sync and lake projection on representative growing transcripts.
- [x] Bound upload preparation memory by a limited batch or file-backed chunks, preserving digest checks, redaction boundaries, retries and rewrite detection.
- [x] Stream lake normalization and Parquet output with bounded buffers while preserving event semantics and generation publication safety.
- [x] Measure incremental projection or batching choices for small appends; document freshness and CPU tradeoffs without changing capture permissions.

## Notes

**agent:codex/memory-e2fc465b** at 2026-10-18T03:28:24Z

The user authorized an epic, child tickets, promotion and autonomous implementation on 2026-10-17. Converted the existing investigation follow-up to the epic and promoted its eight scoped children. Dependencies put reproducible tools before reductions and measurement before runtime tuning. Start with the workload runner, then bounded Parquet serialization; continue through shared agent batching and streamed projection. Retain the investigation as baseline evidence. Do not deploy or change live configuration during this work. Child acceptance criteria are intentionally unchecked until implementation and measurement earn them.

**agent:codex/memory-e2fc465b** at 2026-10-18T03:36:36Z

Refined the agent work into two independently measurable tickets before coding: per-session batch lifetime reduction and TKT-01M56HA9EQBBA9KF4Q9B8PSQ1R (Agent: stream verified artifacts from file-backed snapshots). There are now nine implementation children. The streaming criterion was moved, not discarded, and final validation depends on both phases.

**agent:codex/memory-e2fc465b** at 2026-10-18T03:46:46Z

Reached a tested implementation milestone: three of nine promoted children are complete and released. TKT-01M56GV8XHXWHZ2N41XFP0H8H0 (Perf: reproducible memory and CPU workload runner) provides isolated projection, combined sync and now separate-process agent workloads with profiles and private synthetic fixtures. TKT-01M56GV9BWPGYKMZQ89N221X7X (Parquet: bound serialization and row-group buffers) reduced 128 MiB projection median RSS 690.5→591.2 MiB and Parquet-stage allocation 534.3→252.7 MiB, trading about 1.4% more Parquet storage. TKT-01M56GV9JT00CXN9RE3T6SNRGE (Agent: bound upload batches and release transcript buffers) prepares/sends/ACKs one session at a time; repeated agent-only RSS for three 32 MiB sessions fell 282.6→154.8 MiB, and combined RSS 554.0→432.6 MiB versus the Parquet-only control. These are separate workloads and controls, not percentages to add together. Agent CPU did not improve; combined CPU/allocation ranges overlap. Agent CPU and allocation profiles were successfully parsed, supplementing the existing projection profiles. There is no live heap attribution or production deployment claim.

Acceptance criteria 1 and 2 are satisfied by synthetic measurements/profiles and limited session batching. Criteria 3 and 4 remain open: input/event streaming, bounded single-artifact scanning/transfers, normalize job coalescing/budgets and incremental CPU work are still required. Six children remain queued, including final validation. The next ready high-priority reductions are file-backed agent snapshots and streaming JSONL projection; use the measured controls and preserve redaction, rewrite, retry and generation safety. Diagnostics remain an opt-in tool ticket; live services/config/tokens/capture permissions were untouched. Full repository tests, vet, formatting, application build and targeted race checks pass. Implementation commits include 8456f27, 2616854, f6e52a6 and c9e2404; durable measurement methods/results are docs/performance.md. Releasing the epic claim at this milestone, leaving the epic in-progress and its remaining criteria unticked.

**agent:codex/memory-e2fc465b** at 2026-10-18T03:47:23Z

Completed additional matching profile captures before finalizing the milestone: pre-batching agent control (batch-agent-control-profile32) and updated 128 MiB projection (batch-projection-profile128) both completed, and CPU/alloc_space profiles parsed successfully. Together with batch-agent-profile32 and baseline-profile128, this leaves before/after attribution artifacts for both workloads. Profiles are separate from the unprofiled timing/memory comparisons. Final tool-only race test passes after the RUSAGE_SELF addition. git ticket check has zero errors, the pre-existing long-title warning, and the expected warning that this in-progress epic is intentionally unclaimed between implementation milestones. No acceptance criteria remain claimed by a stale session.

**agent:codex/memory-e2fc465b** at 2026-10-18T04:05:38Z

Completed TKT-01M56HA9EQBBA9KF4Q9B8PSQ1R (Agent: stream verified artifacts from file-backed snapshots), bringing the promoted epic to four of nine children done. Production implementation dca1048 replaces whole artifact/scan-view allocations with private immutable snapshots and bounded ReaderAt scanning; validation/report commit 024ddc8 adds generated 64 MiB single-match/escape benchmarks and complete before/after results. Against the already-batched 680e507 control, three fresh alternating agent-only runs per workload reduced median RSS 157.3→26.6 MiB for three 32 MiB sessions and 549.3→32.3 MiB for one 128 MiB session; stage allocation fell about 96%. Counters, retry/fallback/rewrite safety, full tests, targeted race checks, vet, builds and differential fuzzing pass. CPU did not improve and median cost rose about 14% / 6%; temporary I/O and full-file hashing/copying on appends remain explicit tradeoffs. Live services/config/capture permissions remain unchanged.

The epic stays in-progress with criteria 3/4 open. Five children remain: opt-in diagnostics, streaming lake normalization, normalize coalescing/budgets, incremental projection/search, and final combined validation. Harness-specific export/discovery memory is outside the shared artifact-body bound. The remaining large memory/CPU opportunity is lake whole-session processing and repeated work on small appends. docs/performance.md is the durable measurement/contract record; the child ticket holds rationale and scratch provenance. No deployment was performed.

**agent:codex/memory-e2fc465b** at 2026-10-18T04:40:24Z

All nine promoted children are complete. This session finished streaming JSONL normalization/private output staging, fair byte-weighted same-session admission/coalescing, bounded default-off file diagnostics for agent/lake, the incremental/search CPU evaluation and final release validation. docs/performance.md is the complete contract/evidence/tuning record, docs/performance-results.json preserves per-run observations and binary hashes, docs/incremental-projection.md records checkpoint/reference/search/crash design, and docs/release-notes-memory.md is ready for release review.

Combined agent/lake median RSS fell44% and74% on the final controlled workloads; prior isolated file-backed agent comparisons fell83–94%, with current confirmation around25 MiB RSS. CPU is roughly unchanged for streaming, allocation/GC rises, and temporary disk/cache costs remain explicit. Measured one-worker bursts reduce CPU at a queue/freshness cost. No low hard memory cap or capture-policy change was imposed. Incremental identity/reference changes are deferred to two separately filed draft design/prototype follow-ups, as the evaluation ticket requires; they are outside this bounded-memory release scope.

Full local race/vet/format/build gates and five-target GoReleaser archive/checksum/notices validation passed at45fea68. Both Dockerfile platforms built and amd64 archive/container smoke checks passed. There is no catalog migration, normalizer schema/semantic change or forced requeue. Review/merge and an actual tagged release are next workflow steps; no PR, tag, publish, installation, live restart, live configuration or permissions change was performed. Ticket checking has zero errors and one unrelated pre-existing title warning.

## Summary

Completed all nine children: bounded agent snapshots and lake output streaming, fair coalesced admission, opt-in diagnostics, CPU design evaluation and repeated validation. Reports and release notes are committed; full race/vet/build and archive/container rehearsals pass. No live deployment or release publication.
