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
