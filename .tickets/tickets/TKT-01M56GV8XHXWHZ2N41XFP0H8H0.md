---
schema: 3
id: TKT-01M56GV8XHXWHZ2N41XFP0H8H0
title: "Perf: reproducible memory and CPU workload runner"
type: task
status: ready
status_reason: null
priority: high
due_on: null
labels:
  - area/agent
  - area/normalize
assignees: []
milestone: null
parent: TKT-01M56GHAXEJJXEE45GJ9C37NQG
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-18T03:28:21Z
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

Replace the disposable investigation probe with a checked-in synthetic workload runner and documented commands. Use real normalization, event and Parquet writers, and isolated local upload/lake paths for capture workloads. Support size, session count, repeated small appends and unchanged passes. Report stage wall time, allocation bytes, GC, sampled live heap/RSS and CPU; save opt-in CPU/heap profiles in an explicit private output directory. Record input size, build and runtime settings. Fixture generation is outside measured work and should not construct a second full corpus in memory. Use moderate-diversity deterministic text so compression results are not based only on repeated prose. Outputs must never include live session content, tokens or environment dumps. Stage-level and process-level figures must be distinguished. The runner must never fall back to live configuration or network services. See docs/memory-investigation.md for the baseline and why measurement precedes tuning.

## Acceptance criteria

- [ ] A checked-in runner produces machine-readable stage/process results and optional CPU/heap profiles using only synthetic data and explicit scratch output.
- [ ] Projection and isolated sync workloads support large inputs, multiple sessions, unchanged passes and repeated small appends; exact reproduction commands are documented.
- [ ] Capture at least 32 MiB and 128 MiB baseline results, including allocations, peak sampled memory and runtime settings, without changing live services.
