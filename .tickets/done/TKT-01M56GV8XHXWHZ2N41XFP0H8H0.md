---
schema: 3
id: TKT-01M56GV8XHXWHZ2N41XFP0H8H0
title: "Perf: reproducible memory and CPU workload runner"
type: task
status: done
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
updated_at: 2026-10-18T03:33:40Z
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

- [x] A checked-in runner produces machine-readable stage/process results and optional CPU/heap profiles using only synthetic data and explicit scratch output.
- [x] Projection and isolated sync workloads support large inputs, multiple sessions, unchanged passes and repeated small appends; exact reproduction commands are documented.
- [x] Capture at least 32 MiB and 128 MiB baseline results, including allocations, peak sampled memory and runtime settings, without changing live services.

## Implementation plan

Add cmd/lampi-perf as a developer-only synthetic runner with explicit private output and subprocess-isolated runs. Stream fixture generation to disk outside measured stages; expose projection and combined agent/lake sync scenarios with sessions, unchanged passes and append passes. Record JSON stage allocation/GC/wall/sample peaks and process CPU/RSS separately, runtime/build settings, output size and optional pprof artifacts. All harness homes and lake/state paths are explicit scratch paths. Document commands and profiling overhead. Run small semantic/safety tests, record 32/128 MiB baseline results, and run repository gates before using the runner to judge reductions. No live diagnostics listener or production configuration is needed for this first ticket.

## Notes

**agent:codex/memory-e2fc465b** at 2026-10-18T03:33:39Z

Implemented cmd/lampi-perf and docs/performance.md at 8456f27. Three fresh baseline projection processes per size: median 32 MiB RSS 182.6 MiB / allocation 455.2 MiB; 128 MiB RSS 690.5 MiB / allocation 1641.2 MiB. A combined three-session 32 MiB sync peaked at 529.4 MiB, with zero uploads on unchanged and three blobs/manifests per append pass. Explicit scope avoids misreporting combined sync as agent-only memory. Optional CPU/heap profiles were successfully parsed by pprof. Fixture generation streams to private files outside measured stages; runtime overhead is documented. Unit/safety tests, go vet ./..., application build and go test -trimpath=false ./... pass. The first full test run failed because the machine GOFLAGS=-trimpath breaks existing source-location fixture tests; disabling that flag for the test invocation resolves it without changing machine config. Alternatives: retain the scratch probe (not durable), or use live captures (would require live instrumentation/deployment); the checked-in synthetic runner gives repeatable safe baselines first.

## Summary

Landed the developer-only synthetic projection and combined agent/lake workload runner, safe output handling, machine-readable stage/process measurements, opt-in private profiles, semantic/isolation tests and reproduction/baseline documentation. Baseline measurements establish the starting point for dependent reductions. All required checks pass with the documented test-only trimpath override. Live services are unchanged.
