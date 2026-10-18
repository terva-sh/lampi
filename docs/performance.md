# Measuring memory and CPU

`cmd/lampi-perf` runs the real projection/export or upload/normalization pipeline
against deterministic synthetic Claude transcripts. It never resolves an agent
configuration: every harness home, state directory and lake is explicit, and
the sync workload uses a new ephemeral loopback HTTP listener. The initial
investigation is in [memory-investigation.md](memory-investigation.md).

## Run an isolated comparison

Use Go 1.27 (`mise exec --` if needed). Keep the binary and output under a fresh
agent scratch directory. The `--out` directory must not exist yet: the tool
creates it privately and refuses to reuse any existing directory.

```bash
mkdir -p ~/.cache/agent-scratch/lampi
perf_task_dir=$(mktemp -d ~/.cache/agent-scratch/lampi/perf-XXXXXX)
go build -buildvcs=false -o "$perf_task_dir/lampi-perf" ./cmd/lampi-perf

"$perf_task_dir/lampi-perf" --out "$perf_task_dir/projection32" --size-mib 32
"$perf_task_dir/lampi-perf" --out "$perf_task_dir/projection128" --size-mib 128
"$perf_task_dir/lampi-perf" --out "$perf_task_dir/agent" --workload agent \
  --size-mib 32 --sessions 3
"$perf_task_dir/lampi-perf" --out "$perf_task_dir/sync" --workload sync \
  --size-mib 32 --sessions 3 --unchanged-passes 2 \
  --append-passes 3 --append-kib 64
```

Each invocation runs a new subprocess, prints JSON, and saves the same report in
`results.json` (mode 0600). Synthetic inputs and derived files are deleted at the
end by default; `--keep-data` retains them under `out/data/`. The runner never
removes an existing output directory. It records the build metadata embedded in
the binary; when using `-buildvcs=false`, record `git rev-parse HEAD` and any local
changes beside published results because VCS metadata is not embedded.

The fixture generator writes one record at a time, using deterministic varied
prose with quotes and escaped line breaks. `--size-mib` is a minimum per-session
size rounded up to a complete record. The report records the exact initial
input size. Appends add approximately `--append-kib` per session per pass.
Fixture generation is outside measured stages, but included in process totals.

Projection reports read, normalize, compressed event output and Parquet stages
for each session. Sync reports initial upload, unchanged passes and append
passes, each waiting for normalization to finish. Sync measurements cover the
**combined agent and lake in one process**, including the lake's two normalize
workers. They exclude search indexing and are not agent-only memory figures.
Projection sessions run sequentially; use sync to exercise overlapping workers.

The `agent` workload runs the same sync sequence against a real synthetic lake
in a **separate subprocess**. RSS, heap, stage allocation and profiles describe
only the agent worker. The lake and its normalization workers are excluded.
Stage wall time still includes waiting for normalization after each sync, and
`output_bytes.lake` still reports the lake's disk output. Linux/macOS agent CPU
uses `RUSAGE_SELF` inside the worker, excluding the lake subprocess: some exit
status APIs include waited-for descendants. Agent CPU is reported unavailable
on other systems; inspect `process_cpu_scope`. The child is controlled through
private stdio and listens only on ephemeral loopback, with a 15-minute lifetime
limit. It opens only this run's newly created synthetic lake directory.

## Interpret the report

- Stage allocation is `TotalAlloc` growth during that stage, including concurrent
  workers and the sampler. It is not simultaneous live memory. Stage GC counts
  and heap-after values come from `runtime.MemStats`.
- Peak heap and resident memory are sampled every 10 ms during each stage.
  Sampling can miss a short peak. Heap values include all live allocations in
  the process, including eligible-but-not-yet-collected objects.
- Linux process peak RSS comes from the kernel's lifetime `VmHWM`. It includes
  fixture generation and setup. On systems without `/proc/self/status`, RSS
  fields are omitted; Go heap and process CPU figures remain available.
- Process user/system CPU comes from the worker's exit status, except the
  `agent` mode uses the self-only sampling described above. Process wall/CPU
  includes setup, fixture appends, teardown, sampling and optional profiling.
  Use stage wall times for pipeline comparisons and process totals for the
  complete run's cost. Page cache charged to a cgroup is not process RSS.
- `output_bytes` reports logical file lengths by top-level data component,
  including synthetic inputs. The sync `lake` total includes its database and
  CAS; the projection report separates normalized and Parquet output.

Run repeated separate processes with the same input parameters and runtime
settings. Compare medians/ranges, output size and CPU as well as peak memory.
Do not use a single tight memory limit as evidence of bounded processing.

```bash
GOMEMLIMIT=512MiB "$perf_task_dir/lampi-perf" \
  --out "$perf_task_dir/limited" --size-mib 128
```

## Capture profiles

```bash
"$perf_task_dir/lampi-perf" --out "$perf_task_dir/profile" \
  --size-mib 128 --profiles
go tool pprof -top "$perf_task_dir/lampi-perf" "$perf_task_dir/profile/cpu.pprof"
go tool pprof -top -sample_index=alloc_space \
  "$perf_task_dir/lampi-perf" "$perf_task_dir/profile/heap.pprof"
```

Profiles are opt-in, private files. CPU capture begins after initial corpus
generation. Heap sampling uses a 64 KiB rate, and a final explicit GC updates the
allocation profile outside stage timings. The final heap profile's in-use sample
describes retained state after processing, not the working peak. Profiling and
sampling have overhead; compare unprofiled runs for timing and use profiles to
attribute costs. No profile is captured from a live service by this tool.

## Initial repeatable baseline

Production code at `8456f27`, Go 1.27.1 on linux/amd64, `GOMAXPROCS=12`,
`GOMEMLIMIT` and `GOGC` unset. Three fresh unprofiled projection processes per
size used the default fixture and one session. Values below are medians.

| Transcript | Process peak RSS | Stage allocation total | Stage wall total | Process user + system CPU | Parquet bytes |
| --- | --- | --- | --- | --- | --- |
| 32 MiB | 182.6 MiB | 455.2 MiB | 1.08 s | 1.41 s | 12,681,111 |
| 128 MiB | 690.5 MiB | 1,641.2 MiB | 5.19 s | 6.16 s | 50,650,919 |

The 128 MiB stage wall totals ranged from 4.81 to 9.13 seconds under other
machine load; use timing directionally until repeating an interleaved comparison.
These fixtures have greater text diversity than the disposable investigation
probe, so its timings, compression ratios and memory totals are not directly
comparable. Event IDs are generated by the actual normalizer, which produces
minor output-size variation between runs even with deterministic input.

A separate combined sync run with three 32 MiB sessions peaked at 529.4 MiB RSS.
Its initial pass uploaded six blobs and three manifests, its unchanged pass
uploaded nothing and counted three unchanged sessions, and each of two 64 KiB
append passes uploaded three blobs and three manifests. This validates the
workload shape; it is not an isolated measurement of agent-only RSS. A separate
128 MiB profiled projection was successfully read by `go tool pprof`.

The initial runner's unit tests, `go vet` and application build passed. When a
machine sets `GOFLAGS=-trimpath`, repository tests that use `runtime.Caller` to
find fixture files need `go test -trimpath=false ./...`; the normal build can
keep its configured flags. This changes only the invocation, not live settings.

## Bounded Parquet output

The first reduction keeps indices into the existing event slice rather than a
second full-session collection of serialized rows. One partition writer at a
time uses batches of at most 128 rows or 1 MiB of payload and row groups of at
most 2,048 rows or 8 MiB of payload. A single oversized event is allowed whole.
The input events, small partition-index arrays, compression state and Parquet
footer metadata are outside these payload budgets; full event streaming is
separate work. Event JSON now uses a string builder to avoid an extra copy.
All partitions finish staging before replacement begins, so serialization
failure leaves the previous partitions intact. Replacement remains atomic per
file, as before, rather than a transaction over multiple partition files.

Three fresh projection runs at each size, with the same fixture/runtime
parameters as the baseline, measured the following medians:

| Transcript | Baseline RSS | Bounded Parquet RSS | Baseline Parquet-stage allocation | Bounded Parquet-stage allocation |
| --- | --- | --- | --- | --- |
| 32 MiB | 182.6 MiB | 148.5 MiB | 162.5 MiB | 100.5 MiB |
| 128 MiB | 690.5 MiB | 591.2 MiB | 534.3 MiB | 252.7 MiB |

Total stage allocation at 128 MiB fell from 1,641.2 to 1,359.4 MiB. Median
process CPU fell from 6.16 to 5.45 seconds, but the runs were sequential groups
under varying machine load, so this is not a stable CPU speedup claim. Median
32 MiB process CPU was effectively unchanged (1.41 versus 1.40 seconds).
Parquet file sizes increased about 1.4%, from 50,650,919 to 51,347,718 bytes at
128 MiB, as row groups no longer span the entire session. This trades a small
compression loss for bounded serialized working buffers.

The single combined three-session sync comparison did **not** establish an RSS
improvement: peak RSS was 529.4 MiB before and 543.6 MiB after. Total measured
allocation across its initial/unchanged/two-append passes fell from 5,344.9 to
4,806.4 MiB. Overlapping normalization and GC scheduling still dominate that
process's peak; reducing whole input/event retention and agent batch retention
remains necessary. The projection improvement must not be advertised as an
equivalent improvement for the whole deployed service.

## Agent session batching and shared chunk slices

The agent now prepares, scans, enqueues, uploads and ACKs one session before
preparing the next. Only digest and chunk metadata survive between sessions,
preserving shared-blob deduplication without retaining their bodies. Fixed-size
chunks also share the immutable input snapshot, matching the existing custom-cut
path, instead of copying the entire snapshot into a second set of chunk buffers.
These two changes were measured together. At this stage, every current-session
artifact was still read in full; a tail view could keep that full backing array
alive. Discovery and chunk metadata also scale with corpus size. This bounded
session batch retention; the file-backed implementation below addresses the
remaining artifact and scan buffers.

The comparison uses upload production code before `c9e2404` as its control
(Parquet changes already present), and production code at `c9e2404` as the new
build. **Both** builds use the same measurement tool from `f6e52a6`, including
separate-process agent measurement and the standard agent HTTP transport.
This avoids attributing a tool/transport change to batching. Go/runtime settings
match the initial baseline. Three fresh processes per build per workload ran
in alternating control/new order (reversing order in the second pair).
Each processed three 32 MiB transcripts, one unchanged pass and two 64 KiB
appends per transcript. Values below are medians; ranges are process minima
and maxima across the three runs.

| Measurement | Control | Batching and shared chunks |
| --- | --- | --- |
| Agent-only peak RSS | 282.6 MiB (282.4–284.4) | 154.8 MiB (153.5–156.9) |
| Agent-only sampled peak heap | 230.6 MiB | 129.5 MiB |
| Agent-only stage allocation total | 493.6 MiB | 397.1 MiB |
| Agent-only process CPU | 1.71 s (1.70–1.91) | 1.87 s (1.79–1.91) |
| Combined agent/lake peak RSS | 554.0 MiB (503.5–616.0) | 432.6 MiB (427.5–442.7) |
| Combined stage allocation total | 4,891.5 MiB (4,763.5–4,903.3) | 4,839.2 MiB (4,828.7–4,839.4) |
| Combined process CPU | 15.62 s (15.57–15.65) | 15.20 s (15.15–15.67) |
| Combined stage wall total | 9.38 s | 8.59 s |

Agent-only peak RSS fell about 45% and measured stage allocation about 20%.
The initial pass accounts for almost all the allocation reduction; appends
still read complete files. The combined-process RSS reduction is about 22% in
this workload, with materially variable control peaks. Combined allocation and
CPU ranges overlap. Agent-only CPU did not improve. These results establish a
memory reduction on this synthetic case, not a general CPU speedup or an
expected reduction of the same percentage on the live workstation. Search,
other harnesses and much larger individual sessions still need validation.

Every measured pass preserved its counters: initially six uploaded blobs and
three manifests; unchanged passes uploaded/posted nothing and counted three
unchanged sessions; each append uploaded three blobs and three manifests.
Derived lake storage remained about 70.94 MiB, with small generated-ID variation.
Ordering, retry durability, shared sidecars with/without chunking and no-bay
continuation have dedicated tests. Existing rewrite, tail-prefix fallback,
quarantine, mixed-refusal and unreadable-file coverage runs through the same
preparation path.

A separate single-run scaling check, using the same 32 MiB per-session input
and pass counts, recorded agent-only RSS of 152.6 → 156.3 MiB for one session
and 525.3 → 175.3 MiB for six sessions. The similar one-session peak and
reduced growth across sessions support the batch-lifetime result; these single
runs do not establish a strict memory bound. Kernel RSS also includes the Go
runtime retaining previously used pages, beyond the currently live bodies.

A separate profiled agent run completed and both CPU and allocation profiles
were read successfully with `go tool pprof`. Profiles cover only the agent
worker in `agent` mode; its final heap snapshot is after an explicit GC and
does not describe the peak working set. Full repository tests, vet, formatting,
application build and upload/accept/tool race checks passed for the batching
implementation. Live services and configuration were left unchanged.

Additional pre-batching agent and updated 128 MiB projection captures also
completed and parsed, providing before/after profile artifacts for both scopes.
Post-change agent allocation samples still point to full-file reads and the
JSON-unescape scan view's clone. SHA-256 hashing was the largest CPU sample
category in the short agent capture. These support tackling file-backed
snapshots, bounded scanning and repeated hashing next; removing a safety check
would not be an acceptable substitute for preserving those checks while
streaming.

## File-backed agent artifacts and secret scanning

The agent now copies each admitted session's artifacts into private snapshots
while checking their complete sizes and SHA-256 digests against discovery.
A changed source skips the session for this pass. Scanning, chunk hashing,
HTTP retries, redirects and refused-tail fallback all read ranges of these
verified snapshots; they never reopen the original source path. Full-file
fallback reuses the verified full digest. Capture permissions, quarantine
overrides, redaction rules and the wire protocol are unchanged.

Snapshot copying and range hashing use pooled 64 KiB buffers. The scanner
streams its offset-preserving JSON escape view into another private file,
using 64 KiB input/output buffers and 4 KiB escape padding. Literal lookup uses
a 64 KiB block plus fixed lookahead, anchored regular expressions use a 4 KiB
RuneReader, and append backtracking uses a 64 KiB backward cache. Match state
depends on the fixed ruleset, with a small pending frontier for overlap
selection. A single line, key, PEM block or backslash run can exceed these
buffers without being materialized whole. The existing slice scanner remains
the differential reference. This is a bound on artifact-body buffers, not on
all process memory: discovery, manifests, chunk/digest metadata, runtime caches
and the number of open snapshot descriptors still scale with their counts.
Current-session artifacts each hold one descriptor until that session finishes.

The cost is temporary disk I/O. Storage during a session is bounded by its raw
artifact sizes plus the current artifact's scan view (at most its full size);
an append normally stages only its overlapping suffix as the view, but still
snapshots and verifies the complete source. Files use mode 0600 inside a 0700
session directory under the lake-specific agent state. Normal completion,
quarantine, errors and cancellation close descriptors and remove the directory.
On Unix, open snapshots and views are unlinked immediately, so process death
also releases their bytes; an empty session directory can remain. On Windows,
private named files can remain after a crash. Kernel page cache and memory
charged to a cgroup are separate from the agent RSS reported here; lower RSS
does not mean these temporary writes have no memory or storage cost.

Production baseline `680e507` (already batched) and snapshot implementation
`dca1048` use the same runner and Go/runtime settings as above. Three fresh
processes per build per workload ran in alternating control/new order, reversing
the second pair. Each workload included one unchanged pass and two 64 KiB
appends per session. The figures below are medians; parenthesized RSS and CPU
values are the full ranges across the three runs.

| Measurement | Three 32 MiB sessions: batched | Three 32 MiB sessions: snapshots | One 128 MiB session: batched | One 128 MiB session: snapshots |
| --- | --- | --- | --- | --- |
| Agent peak RSS, MiB | 157.3 (153.4–186.4) | 26.6 (24.9–30.7) | 549.3 (548.6–550.2) | 32.3 (25.6–33.0) |
| Sampled peak heap, MiB | 133.4 | 3.4 | 527.4 | 3.5 |
| Stage allocation total, MiB | 397.4 | 14.8 | 532.5 | 21.0 |
| Agent process CPU, s | 1.92 (1.70–3.08) | 2.19 (1.98–2.23) | 2.55 (2.54–2.75) | 2.70 (2.63–2.77) |
| Stage wall total, s | 8.82 | 8.92 | 18.07 | 17.84 |

These synthetic agent-only RSS reductions are about 83% and 94% relative to
the already-batched control. Stage allocation fell about 96% in both cases.
They are not projected reductions for the deployed agent or combined lake.
CPU did not improve: median agent CPU rose about 14% and 6%, respectively,
with overlapping ranges. Small appends still copy and hash the complete
artifact. Removing that repeated work needs a separate design that preserves
verification. Lake normalization and search remain separate work.

One three-session control run took 100.59 seconds (other controls 8.65–8.82),
including long append waits, and allocated 492.5 MiB (others 397.2–397.4).
It remains in the ranges and medians. Stage wall includes lake normalization
waits; allocation/CPU include the agent's sampler while waiting, so varying
machine load can inflate these totals. New-build wall totals ranged
8.73–9.41 seconds for three sessions and 17.79–18.40 for the single large
session. No general CPU or latency improvement is established.

Input sizes, runtime settings and all stage transfer counters matched across
builds and repetitions. Initially the three-session workload uploaded six
blobs and three manifests. The 128 MiB workload uploaded five blobs (including
chunks) and one manifest. Unchanged passes uploaded/posted nothing; each append
uploaded/posted once per session. There were no refusals, quarantine hits,
skips or no-bay outcomes. Derived lake output stayed about 70.94 and 94.09 MiB,
respectively, with small generated-ID variation.

Separate before/after agent profiles at three 32 MiB sessions were captured
and parsed with `go tool pprof`. The control attributed about 289 MiB to
`readArtifacts` and 97 MiB to the JSON view during scanning. The new scanner
attributed about 2.6 MiB to `ScanReaderAt`; whole-artifact reads and scan-view
clones no longer dominate. Allocation profiles also include about 81 MiB of
fixture generation in both builds; their totals are not stage allocation.
Short CPU captures still show SHA-256 work, with increased syscall work in
the new implementation, consistent with the temporary-file tradeoff.

`BenchmarkReaderScanLongSpans` generates a single long key or backslash run
through `ReaderAt`, without allocating the complete input. Three one-iteration
runs per case allocated 276–282 KiB per scan for both 1 MiB and 64 MiB inputs,
and each found the expected single hit. This separately exercises unbounded
match/escape lengths, beyond the normal-record workload above:

```bash
TMPDIR="$perf_task_dir" go test -trimpath=false ./internal/redact -run '^$' \
  -bench '^BenchmarkReaderScanLongSpans$' -benchtime=1x -count=3
```

Differential tests cover all rules and transcript wrappers, append/block
boundaries, long spans, published examples, invalid UTF-8 and overlapping hits.
Two short differential fuzz runs completed 141,570 and 113,398 executions
without mismatches. Read-error and cancellation tests fail closed, including
errors that the regular-expression reader would otherwise interpret as EOF.
Upload tests cover source replacement after scanning, same-size rewrites,
whole/chunk/range uploads, retries, redirects, full-file fallback, partial
snapshot failure and cleanup on success, quarantine, no-bay, cancellation and
network failure. Linux cleanup assertions also check for leaked snapshot file
descriptors. Full repository tests, targeted upload/redaction/accept/tool race
tests, vet, formatting and application/tool builds passed. These changes have
not been deployed; live services, configuration and capture permissions were
left unchanged.

## Optional local runtime captures

Both `terva-lampi agent` and `terva-lampi serve` accept
`--diagnostics-dir NEW_DIR --diagnostics-duration 30s`. The directory must not
exist; its parent must exist. Capture is off when the directory is omitted.
Duration must be between 1 second and 5 minutes. Capture stops automatically
or early on process shutdown, flushing `cpu.pprof`, `heap.pprof` and
`runtime.json`. Files are mode 0600 in a mode 0700 directory on Unix. An
existing path, including a symlink, is refused. No listener, HTTP pprof route,
command-line dump, environment dump, goroutine dump or raw heap dump is added.
Profiles contain sampled stacks and executable symbols, not allocated object
contents. Keep them private because code paths and source locations may still
identify local software. A capture failure is reported at startup or shutdown.

CPU profiling has overhead. Heap/allocation profiles use Go's default sampling
rate and can be inspected with `go tool pprof -inuse_space` or `-alloc_space`;
they are estimates, not complete object inventories. No forced GC is performed
by production capture. `runtime.json` reports start/end cumulative allocation,
GC cycles/pause time, Go-managed memory, and process CPU (Linux/macOS) and RSS
(Linux). Fields unavailable on another platform are absent or marked unavailable.
A capture observes one process, including all its goroutines.

The existing optional lake `/metrics` listener also exposes
`lampi_runtime_heap_bytes`, `lampi_runtime_memory_bytes`, cumulative allocated
bytes, GC cycles and GC pause seconds, plus process resident bytes on Linux and
process CPU seconds on Linux/macOS. These aggregate observations contain no
session IDs or content. Scrape at the existing recommended interval of 15
seconds or slower. The diagnostics flags do not enable the metrics listener.

Heap bytes can include unreachable objects awaiting collection. Go-managed
memory is runtime memory less released heap pages. RSS includes resident Go
memory, stacks and other mappings; it is not a Go heap limit. A service cgroup
can additionally charge filesystem page cache from snapshot/spool/CAS writes.
That cache is reclaimable and is not included in process RSS. `GOMEMLIMIT` is a
soft Go runtime target, not a hard RSS or cgroup bound.

To test the production capture implementation without live credentials, run
`lampi-perf --workload sync --size-mib 1 --out NEW_DIR --diagnostics`.
The synthetic worker writes a `diagnostics/` directory and stops capture when
its workload ends. Choose either `--diagnostics` or `--profiles` per run.

The runner also accepts `--burst-passes N` to append and sync without waiting
between generations, followed by a final drain. `--normalize-workers N` and
`--normalize-input-budget-mib N` select admission settings for the isolated
lake. Per-drain results include job outcomes, coalescing and queue wait totals
and maxima. The workload fails if normalization cannot drain.
`--workload sync --search` separately times real search reconciliation after
each drained sync and rejects incomplete index coverage. Per-stage CPU and
Linux `/proc/self/io` differences distinguish logical read/write characters
from storage I/O; warm page cache can make `read_bytes` zero even when the
application reads every byte.

`--workload stream` performs independent parse-only, parse plus indexed-event
encoding, and full parse/stage/Parquet/publication passes. They repeat parsing
and overlap in work: their times must not be summed, or subtracted as an exact
concurrent pipeline decomposition. The older `projection` mode retains the
slice API for compatibility/control measurements; use `sync` for the actual
streaming worker. Profile runs are separate from unprofiled comparisons.

## Streaming lake contract and admission

The worker now uses logical CAS readers (including chunked objects) and streams
Claude/Codex/terva transcript JSONL and terva error JSONL. It retains the largest
record/event and parser state, indexed-zstd frame buffers, one row spool buffer,
and one bounded Parquet row group. Row batches retain the existing 128-row / 1
MiB target and row groups the existing 2048-row / 8 MiB target; a single larger
row is admitted. Frame/partition metadata scales with counts. This is not a
constant bound for one arbitrarily large JSON record. Other harnesses still use
their compatibility slice projectors, and callers of the slice `Project` API
can still collect the complete event list.

Partition rows spool as private length-prefixed strings, avoiding a second JSON
escape/parse cycle. At most one spool and one Parquet writer are open. Private
stages contain complete output before generation/head validation under the
session publication lock. Publication is atomic per file, followed by catalog
readiness; it is not a transactional directory swap. Failed/cancelled/panicking
or superseded stages are removed. Failed rename publication removes derived
outputs and leaves the durable job for retry. CLI startup under lake.lock
removes abandoned `.projection-*` directories before starting workers.
Raw CAS data is never modified by projection.

Temporary disk can include row spools plus compressed events and candidate
Parquet files alongside the old generation. Spools repeat full event JSON and
searchable text, so input bytes are not an adequate temporary-space quota.
Files are 0600 under a 0700 stage directory in the lake's own data filesystem.
Plan disk headroom and observe storage; no temporary disk cap was introduced.

`serve --normalize-workers 2 --normalize-input-budget-mib 256` is the default.
Admission estimates all current logical artifact bytes conservatively and
reloads those weights from the catalog after restart. It coalesces pending
same-session generations in their original queue position and never runs two
for that session together. Oversized jobs run alone. Up to eight smaller jobs
can bypass a budget-blocked session before it reserves the next capacity.
Delayed retries retain their hold; obsolete timers cannot replace newer work.
Latest-generation/head checks and the durable job table remain authoritative.
The byte budget controls overlap, not record sizes, codec caches, RSS or cgroup
memory. Worker count remains an independent upper bound.

Metrics include admitted/budget input bytes, coalesced jobs and queue-wait
sum/count/maximum. Queue wait is measured from first pending insertion to
admission (including replaced generations' time in that position), and excludes
delayed retry timers and projection time. The maximum is since process start.
An empty queue alone does not prove successful publication: also inspect job
outcomes and catalog normalization readiness.

## Final lake comparison

The control uses production `10ee962` (file-backed agent, slice lake), and the
streaming/admission build uses production code committed as `635dea4`. Both
binaries were built with `-buildvcs=false` while the production changes were
being committed. The final evaluation tool uses `334cb23` plus the later
restart-test-only change; diagnostics are disabled unless requested. Binary
SHA-256s and per-run measurements are preserved in
[performance-results.json](performance-results.json). Later changes reject
truncated private spools and add diagnostics; they do not change successful
normalizer semantics. Private profiles and synthetic fixtures are disposable
under `~/.cache/agent-scratch/lampi/epic-finish-eh56lo4x/`.

Three fresh processes per build/workload ran in alternating control/new order,
reversing the second pair. Each included initial sync, one unchanged pass and
two 64 KiB append passes per session. This is combined agent/lake memory and
CPU, including normalization and excluding search. Go 1.27.1 linux/amd64,
GOMAXPROCS=12, default GOGC/GOMEMLIMIT, same machine/corpus. Machine load and
filesystem cache were not isolated; no host cache was dropped. Brief targeted
tests and a small diagnostic smoke capture also ran during this series, so
small CPU differences are not evidence of a speed change. Profiles are separate.

| Measurement | 3 × 32 MiB control | 3 × 32 MiB streaming | 1 × 128 MiB control | 1 × 128 MiB streaming |
| --- | --- | --- | --- | --- |
| Peak RSS, MiB, median (range) | 418.2 (404.2–440.3) | 235.2 (223.2–243.4) | 665.1 (663.0–727.0) | 174.2 (145.7–178.4) |
| Sampled peak heap, MiB | 368.1 | 222.5 | 555.9 | 159.3 |
| Stage allocation total, MiB | 4520.9 | 4930.9 | 4757.3 | 5606.6 |
| Process CPU, s, median (range) | 14.98 (14.96–15.02) | 15.26 (15.08–15.48) | 19.31 (18.89–19.44) | 19.44 (19.37–19.54) |
| Stage wall total, s | 8.16 | 8.54 | 17.29 | 17.88 |
| Stage GC cycles | 46 | 92 | 30 | 177 |
| Retained fixture/lake/agent output, MiB | 167.4 | 167.4 | 222.2 | 222.2 |

Median RSS fell about 44% and 74%. Total allocation rose about 9% and 18%, and
GC cycles increased as the collector worked with a smaller live set. CPU medians
rose about 2% and 1%, and wall time about 5% and 3%; this establishes memory
improvement, not general CPU acceleration. The initial JSON row spool prototype
allocated and consumed substantially more CPU; replacing it with length-prefixed
strings avoided reparsing/escaping full event JSON. The final implementation
still performs two event serializations and temporary spool I/O.

Inputs and transfer counters matched: unchanged scans transferred/posted nothing;
each append posted once per session, with no quarantine/refusal/skip outcomes.
Small output differences reflect generated IDs/compressed bytes. Retained output
includes synthetic source files and CAS, not just derived data, and is not peak
temporary disk use. The source corpus uses ordinary ~4 KiB Claude JSON records;
these figures do not establish the working set of a single giant record or of
other harnesses' compatibility projectors.

## Admission, memory-limit and debounce choices

Three fresh-process repetitions used three 16 MiB sessions, initial sync and
8 × 64 KiB appends per session without normalize waits, then a final drain.
The second repetition reversed setting order. Values below are medians; all
runs had zero failed/retried jobs. Job counts include initial work and
superseded generations. A superseded job may already have performed projection,
so coalescing earlier can save substantially more CPU than discarding at publish.

| Setting | RSS MiB | Sampled heap MiB | Allocation MiB | Process CPU s | Stage wall s | Admitted / coalesced jobs | Mean / maximum admission wait s |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 2 workers, 256 MiB input budget | 318.5 | 265.2 | 8010.6 | 23.44 | 9.13 | 24 / 3 | 0.42 / 0.98 |
| 1 worker, 256 MiB input budget | 248.3 | 196.3 | 5470.0 | 16.76 | 10.20 | 15 / 12 | 1.02 / 1.68 |
| 2 workers, 32 MiB input budget | 243.9 | 202.3 | 5518.2 | 16.98 | 10.36 | 15 / 12 | 1.02 / 1.88 |
| 2 workers, 256 MiB input budget, GOMEMLIMIT=256MiB | 270.0 | 242.8 | 8021.8 | 23.54 | 9.20 | 24 / 3 | 0.42 / 1.01 |
| 2 workers, one sync after the whole append burst | 233.4 | 209.5 | 1970.3 | 5.95 | 3.21 | 6 / 0 | 0.06 / 0.20 |

One worker or the smaller input budget saved about 28% CPU and 22–23% RSS,
but increased stage wall time about 12–14% and queue wait. Throughput for this
fixed 27-submission workload is roughly 3.0 submissions/s at the default and
2.6 at the smaller budget; admission/job completions alone are misleading
throughput measures because the number of useful/superseded/coalesced jobs varies.
The 32 MiB budget admits only one of these slightly-over-16 MiB sessions at a
time. It is an experimental control, not a universal recommended byte value.

`GOMEMLIMIT=256MiB` reduced burst RSS about 15% at similar CPU/wall time here,
but RSS still exceeded 256 MiB. It did not remove repeated projection work.
Use the two-worker default when freshness matters and one worker as the first
explicit low-resource option when longer queues are acceptable. Consider a
moderate memory target only after measuring actual record sizes, other harnesses,
search and cgroup cache. Do not translate the synthetic heap directly into a
systemd/container hard limit. There is no evidence for imposing a lower cap on
the agent, whose measured working set is already small.

The last row models coalescing at the source using `--burst-final-only`; it
appends identical bytes but syncs only after the eighth append. It reduces CPU
about 75% but delays the first seven appends until the final sync. It is not a
measurement of a specific watch timer or an arrival schedule. Production keeps
its 5s quiet window / 30s maximum wait. If an operator chooses 10s/60s for a
busy session, the possible added quiet/continuous-write delay is 5s/30s before
network, normalization and search. No measured result here justifies changing
that default automatically. A tighter debounce does the opposite: more frequent
visibility with more full-file verification and full-session projection.

Rollback a tuning experiment if oldest pending age grows without recovery,
latest-session readiness/search coverage lags the chosen freshness objective,
CPU/GC rises, temporary disk fills, or upload/quarantine counters change.
Restore the prior worker/budget/GOMEMLIMIT/debounce values together, then verify
that durable pending jobs drain. Removing the new flags is necessary when
returning to an older binary. Tuning recommendations are optional; live values
have not changed.

## Remaining CPU and incremental design

Three independent 128 MiB streaming component runs measured the following
medians. Each pass rereads and reparses the source; these are overlapping
experiments, not additive pieces of a single run.

| Pass | Wall s | Process CPU s | Allocation MiB | GC cycles | GC pause s |
| --- | --- | --- | --- | --- | --- |
| Parse to discard | 2.22 | 2.54 | 1003.0 | 344 | 0.020 |
| Parse plus indexed-zstd events | 3.04 | 3.17 | 1050.4 | 51 | 0.004 |
| Parse, spool, events, Parquet and publish | 4.73 | 4.91 | 1804.5 | 79 | 0.006 |

A representative complete pass read about 400 MiB logically and wrote about
343 MiB, compared with 128 MiB read by parse-only. Its storage `read_bytes` was
zero because the fixture and spool were cached. Writes include temporary spool
and derived output and do not describe retained size. Logical `/proc/self/io`
counts include sampler/SQLite/network operations where present, and storage
write counters are not a count of completed physical-device writes. GC pause
is stop-the-world time, not total concurrent GC CPU.

A separate two-worker CPU profile (three 32 MiB sessions with two appends)
attributed about 31.7% of samples cumulatively to Claude line parsing, 25.8% to
zstd encoding, 18.3% to the Parquet stream path, and 9.7% flat to the Linux
syscall entry. Background GC accounted for about 1.5% cumulatively. These
categories overlap (Parquet includes zstd and I/O); do not add them. Spool
writing/reading and repeated event encoding remain measurable costs. Allocation
profiles still include fixture setup and runtime/codec caches, whereas stage
allocation totals exclude fixture setup.

Three real 32 MiB sync/search runs measured median append sync+normalization
CPU of 1.63 and 1.57 s for the two 64 KiB appends; separately timed search passes
used 0.42 and 0.34 s, allocating about 161 MiB each. Initial indexing used
2.81 CPU s; an unchanged search reconciliation used 0.11 CPU s with background
index maintenance. All passes reached complete index coverage. Search already
skips unchanged SQL row writes, but still decodes the event prefix and can do
FTS merge/WAL work even in an unchanged pass. This workload does not prove the
benefit of bypassing a prefix or separate all FTS maintenance from decoding.

The decision and complete checkpoint/search-prefix design are in
[incremental-projection.md](incremental-projection.md). Transparent prefix reuse
would change random event IDs, projection ingestion timestamps and source-digest
references. The memory release keeps those semantics. Two draft design/prototype
follow-ups specify identity/reference decisions, authenticated prefix proof,
parser/partial-record/sidecar state, rewrite/truncate invalidation, supersession
and crash recovery, with 50% append CPU targets. Full projection is retained
until those proofs and semantic decisions exist.

Current agent-only confirmation, three fresh processes per workload, measured
median RSS 24.8 MiB (24.2–25.7) for three 32 MiB sessions and 25.4 MiB
(24.0–26.2) for one 128 MiB session. Sampled heap was 3.4/3.5 MiB, stage
allocation 14.6/22.5 MiB, process CPU 1.91/2.54 s, and stage wall 8.87/19.03 s.
These confirm the earlier file-backed reduction; they are not an additional
controlled speed comparison. The agent process excludes lake CPU/heap/RSS but
its stage wall waits for normalization, so it can vary with lake/machine load.

Six additional retained-data runs (three 32 MiB sessions, three fresh processes
at default and GOMEMLIMIT=256MiB) measured Linux file residency with `fincore`
after exit, deduplicating inode identities. About 167.45 MiB of private source,
CAS, derived and agent-state pages remained resident under either setting
(42 files per run; sizes are page-rounded). All three sessions in every catalog
had published gen/head matching current gen/head, no normalization error, and
zero durable pending jobs. These are cached file pages, separate from the
process RSS peak; the processes had already exited. They are not a measured
cgroup peak or exclusive ownership of shared pages. Deleted temporary stages
are absent from this sample, and no global cache was dropped. A runtime soft
heap target does not cap these cached file pages. Post-exit cache residency
and the full per-run runtime/allocation/CPU/wait/transfer observations are in
[performance-results.json](performance-results.json).

Aggregate stage allocation rates in the combined comparison were roughly
554→578 MiB/s for three sessions and 275→314 MiB/s for one large session
(total allocation / summed measured wall). They describe allocation churn,
not a retained-memory budget. The isolated process scopes, unprofiled medians,
profile overhead, largest-record limit, uncontrolled host load/cache, synthetic
harness/record mix and absence of production deployment all constrain what can
be inferred from these results.

## Release readiness

Local release rehearsal at `45fea68` passed:

- Formatting and whitespace checks, `go vet ./...`, and the full
  `go test -trimpath=false -race ./...` repository suite.
- Application/tool builds and `goreleaser check`.
- A non-publishing GoReleaser snapshot for linux/amd64, linux/arm64,
  darwin/amd64, darwin/arm64 and windows/amd64. All five archives contained
  the binary, LICENSE and README; every archive matched checksums.txt.
- Extracted linux/amd64 archive `--version` and healthcheck-help smoke tests.
- The repository Dockerfile built with Buildah for linux/amd64 and linux/arm64,
  followed by amd64 container version and healthcheck-help smoke tests.

The only test flag adjustment is the documented host `-trimpath=false` fixture
lookup requirement. The streaming component measurements read raw fixture files;
production also incurs CAS decompression and API/catalog work. Tests and release
builds ran after the timing comparisons. Profiles were separate from unprofiled
medians. No source changes followed the passing gates; subsequent commits close
records and add documentation.

Artifacts and logs are private/disposable under the task scratch directory
above. A scratch copy of `.goreleaser.yaml` only redirected `dist` there; repository
release configuration is unchanged. `0.8.1-next` is the snapshot archive name,
not a selected release tag. The archive binary recorded the clean source revision;
the future tagged version identity check remains part of the actual release CI.
`git ticket check` has zero errors and one pre-existing long-title warning on
TKT-01M3K38AAK8Q9P3RB6G8GP9JZ9, outside this epic.

[Prepared release notes](release-notes-memory.md) state that no catalog migration,
normalizer semantic/schema change or forced re-normalization is introduced.
Review/merge onto the release branch and the normal tagged release workflow are
still separate steps. No tag, forge release, image push, installation, live
restart, configuration change or capture-policy change was performed.
