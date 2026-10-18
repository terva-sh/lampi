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
These two changes are measured together. Every current-session artifact is still
read in full; a tail view can keep that full backing array alive. Discovery and
chunk metadata also scale with corpus size. File-backed artifact streaming is
separate work, so this is a bound on session batch retention, not a fixed memory
ceiling for an arbitrarily large session.

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
