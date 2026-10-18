---
schema: 3
id: TKT-01M56HA9EQBBA9KF4Q9B8PSQ1R
title: "Agent: stream verified artifacts from file-backed snapshots"
type: task
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area/agent
  - area/redact
assignees: []
milestone: null
parent: TKT-01M56GHAXEJJXEE45GJ9C37NQG
origin: null
dependencies:
  - TKT-01M56GV9JT00CXN9RE3T6SNRGE
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-18T03:36:34Z
updated_at: 2026-10-18T04:05:38Z
created_by:
  id: agent:codex/memory-e2fc465b
  name: ""
updated_by:
  id: agent:codex/memory-e2fc465b
  name: ""
extensions: {}
---

## Description

Split the single-artifact streaming phase from TKT-01M56GV9JT00CXN9RE3T6SNRGE (Agent: bound upload batches and release transcript buffers) before implementation. Batching bounds how many sessions are prepared concurrently; it does not bound the largest session or remove a full-file read. These require different correctness arguments and measurements, and neither acceptance criterion should disappear into the other.

Use immutable private file-backed snapshots or verified chunks so digest checking, secret scanning, PUT retries and tail-prefix fallback see the same bytes. Hash and scan with bounded buffers, preserving the boundary/backtracking rules for long secret patterns and JSON escapes. Fall back safely when a source file changes; never upload unverified newer bytes from a reused path. Existing TKT-01M44DRPS (Cursor CLI: stream the export so large stores need not be capped) remains the export-specific work and should reuse the shared path where practical.

Record before/after agent allocation/RSS and small-append CPU. A tail slice retaining the full original byte buffer is insufficient; a lower GOMEMLIMIT without a working-set reduction is also insufficient.

## Acceptance criteria

- [x] Large shared artifact transfers use bounded file-backed/chunk buffers rather than a full transcript allocation; document the largest-record/chunk memory contract.
- [x] Preserve complete digest verification, same-size rewrite detection, redaction across boundaries, retries and full-file fallback when a tail is refused.
- [x] Isolated large-artifact and boundary/rewrite/retry tests plus comparable profiles demonstrate the memory bound without changing capture permissions.

## Implementation plan

Replace in-memory prepared artifact/tail bodies with immutable private file snapshots and byte-range sources. Copy/hash with fixed buffers and reject a snapshot whose size/digest no longer matches discovery; scan and every retry/fallback read only that snapshot. Scope cleanup to each admitted session, including quarantine, cancellation, enqueue/send failures and ACK. Reuse full verified digests when widening tails, stream HTTP bodies with replayable file ranges and preserve range/chunk resume and custom cuts.

For redaction, keep the existing byte-slice API as the reference and add a file/ReaderAt API. Build its offset-preserving JSON escape view into a second private temporary file using bounded buffers, then scan literal prefixes in blocks. Evaluate anchored regular expressions through a buffered RuneReader for arbitrarily long matches; compare documented examples without retaining key material and select hits with a bounded frontier rather than collecting all spans. Recover append overlap/backtracking from ReaderAt with bounded buffers, preserving existing ScanAppended boundaries exactly. This pays temporary disk I/O to avoid both full transcript and unescape-clone allocations; record that tradeoff and measure CPU rather than assuming it improves.

Add differential tests against Scan/ScanAppended (all fixtures/rules, JSON escapes, long runs, PEM, overlaps/examples, invalid UTF-8, chunk and append boundaries), source mutation/rewrite/retry/fallback/cleanup tests and an isolated large-artifact memory measurement. Use baseline 680e507 with the same runner, repeat interleaved agent workloads at 32 and 128 MiB, retain counter/output comparisons, profiles and full repository/race/vet/build gates. Do not modify live services or capture permissions. Snapshot and scan buffers are fixed-size; artifact/chunk/discovery metadata still scales with counts and is outside the body-buffer contract.

## Notes

**agent:codex/memory-e2fc465b** at 2026-10-18T03:46:45Z

The prerequisite batching ticket is done at c9e2404 with comparable agent-only measurements via the f6e52a6 runner. docs/performance.md records medians/ranges; scratch artifacts are /home/sothr/.cache/agent-scratch/lampi/implementation-gACllt/batch-agent-profile32 and batch-agent-* JSON reports. In one profiled three-session 32 MiB run, allocation samples attribute about 289 MiB to readArtifacts via os.ReadFile and about 97 MiB to redaction/jsonView during Scan/ScanAppended; the JSON view still clones the scanned text. The profile includes fixture generation (about 76 MiB), so do not present its total as stage allocation. CPU capture covers only agent work after initial corpus generation: SHA-256 block work was 47% of 870 ms samples and redact.find about 8%; this short sample is directional. Removing complete artifact reads alone will not bound the scanner's JSON-unescape clone. Preserve the existing cross-boundary and escaped-JSON secret tests while designing streaming snapshots/scans. One-session RSS was effectively unchanged by batching (152.6→156.3 MiB), while six-session RSS fell 525.3→175.3 MiB; batching solved batch retention, leaving this artifact/scanner work necessary.

**agent:codex/memory-e2fc465b** at 2026-10-18T03:55:41Z

Implemented a bounded ReaderAt scanner alongside the unchanged slice reference API, plus session-owned private snapshots and replayable HTTP byte ranges. Differential tests pass across all rule/context fixtures, buffer/append boundaries, unbounded whitespace/slash/header/escape runs, long PEM bodies, invalid UTF-8 and many hits. A 10-second differential fuzz run completed 141,570 executions without mismatch. Existing upload/accept/runner tests pass after adapting private body types; new tests cover source path replacement after scan during whole/chunk/range upload and tail fallback, snapshot cleanup on success/no-bay/quarantine/failure/cancellation, private permissions, and file-backed range retry.

The first unprofiled three-session 32 MiB probe reduced agent peak RSS to 25.3 MiB and stage allocation to 14.3 MiB, versus prior batching medians 154.8 MiB / 397.1 MiB. This single run is provisional; repeated comparisons and larger inputs follow. Snapshot bytes and JSON view files are private, scoped to one session and removed after its consumer; cleanup errors now propagate. Files are 0600 and directories 0700, all later snapshot opens are read-only. View construction adds temporary disk I/O, while artifact/chunk/discovery metadata remains outside the body-buffer bound. Live config/services remain unchanged. Scratch and the baseline binary are in /home/sothr/.cache/agent-scratch/lampi/agent-stream-n0kc42ns.

**agent:codex/memory-e2fc465b** at 2026-10-18T04:05:37Z

Implementation is committed at dca1048; additional long-record/read-error validation and the durable report are at 024ddc8. Private read-only file snapshots replace artifact byte slices. All retries, redirects, chunks, ranged PUTs and refused-tail fallback use the verified snapshot. Copy/hash/scanner buffers are fixed-size, while discovery/manifest/chunk metadata and snapshot descriptor counts still scale. Snapshots are scoped to one session; partial errors, quarantine, cancellation, no-bay and network failure close handles. Unix unlink-on-open releases bytes after process death; Windows can retain private names after a crash. Temporary storage is session raw sizes plus the current scan view. docs/performance.md states this body-buffer contract, including page-cache/cgroup and adapter/discovery limits.

Alternatives: reopening original files for retries could send bytes that were never verified or scanned, so every consumer uses immutable snapshots. A tail slice still retained its full backing allocation. A fixed lookbehind/maximum-line scanner would miss arbitrarily long key/escape/header runs, so the new ReaderAt path preserves backtracking and evaluates anchored expressions through a buffered RuneReader. Staging the JSON view pays disk I/O but preserves exact slice-reference offsets without a full clone or a large-record cap. No safety check, capture permission or ruleset was relaxed.

Fresh alternating comparisons against the already-batched 680e507 control ran three processes per build for three 32 MiB sessions and one 128 MiB session, each with one unchanged and two 64 KiB append passes. Median agent RSS: 157.3 to 26.6 MiB (ranges 153.4–186.4 / 24.9–30.7), and 549.3 to 32.3 MiB (548.6–550.2 / 25.6–33.0), about 83% / 94% lower. Stage allocation: 397.4 to 14.8 MiB and 532.5 to 21.0 MiB. Sampled peak heap: 133.4 to 3.4 MiB and 527.4 to 3.5 MiB. CPU did not improve: median 1.92 to 2.19 seconds and 2.55 to 2.70 seconds. One control had long append waits (100.59 seconds total); it remains in ranges/medians. Sampler allocation/CPU grows during waits, and stage wall includes lake normalization. All input sizes, runtime settings and stage counters matched. Initial three-session counts were 6 blobs/3 manifests; initial 128 MiB counts were 5 blobs including chunks/1 manifest. Unchanged passes uploaded nothing; appends uploaded once per session. Lake output sizes stayed about 70.94 / 94.09 MiB with generated-ID variation. These are agent-only synthetic results, not live or combined-service reductions.

Matching CPU/allocation profiles parsed successfully. The control attributed about 289 MiB to readArtifacts and 97 MiB to scan JSON-view cloning; the new ScanReaderAt attributed about 2.6 MiB, and those full-body sites no longer dominate. Both profiles include about 81 MiB of fixture generation outside stage allocation. Short CPU samples show continued hashing plus increased syscall work. Scratch binaries, reports, logs, comparison-summary.json and before/after profiles are under /home/sothr/.cache/agent-scratch/lampi/agent-stream-n0kc42ns. Reproduce from the report and these revisions; scratch is disposable.

All repository tests, upload/redact/accept/tool race checks, vet, formatting, application/tool builds and added scanner read-error race check passed. Differential fixtures cover every rule/context, block/append boundaries, published examples, long spans/escapes, overlaps and invalid UTF-8. Two short fuzz runs passed 141,570 and 113,398 executions. The long-span benchmark generated single 1 MiB and 64 MiB matches/escape runs without allocating input: all expected hits, 276–282 KiB allocated per scan in three runs per case. Source replacement/same-size rewrite, range retry, redirect replay, tail fallback, permissions, partial snapshot failure and cleanup tests pass, including Linux descriptor leak checks. No live service, config, token or capture permission was changed.

## Summary

Completed verified file-backed artifact snapshots, bounded ReaderAt secret scanning and replayable HTTP ranges at dca1048, with long-record/read-error validation and the measurement report at 024ddc8. Three-run agent-only comparisons against the already-batched control reduced median RSS 157.3→26.6 MiB for three 32 MiB sessions and 549.3→32.3 MiB for a 128 MiB session; stage allocation fell about 96%. Transfer counters and safety tests match. Full tests, targeted race checks, vet, builds, differential fuzzing and 64 MiB long-match benchmarks pass. CPU did not improve; temporary I/O and full-source verification remain costs. docs/performance.md records methods, ranges, limits and disk/descriptor contracts. Live services and capture permissions are unchanged.
