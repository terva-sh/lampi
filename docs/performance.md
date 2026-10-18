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
- Process user/system CPU comes from the worker's exit status. Process wall/CPU
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
