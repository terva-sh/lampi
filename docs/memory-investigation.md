# Memory and CPU investigation, 2026-10-17

The main opportunity is to bound whole-session processing and avoid repeating
it for small appends. Both the capture agent and the lake allocate memory in
proportion to large transcripts. The lake also repeatedly parses, serializes,
compresses and indexes existing content after a small upload.

Follow-up: TKT-01M56GHAXEJJXEE45GJ9C37NQG (Bound memory and repeated work for
growing transcripts), initially filed in draft and subsequently promoted to an
epic at the user's request; splitting batching from artifact streaming brought
it to nine implementation tickets. The repeatable workload runner and measured
Parquet, batching and file-backed agent changes are documented in
[performance.md](performance.md).
No service, live configuration, token,
allow rule or capture permission was changed during this investigation.

## Live observations

The checkout was clean at the start, at `f33fda6`. The running processes were:

| Process | Build | RSS observations | Lifetime peak RSS |
| --- | --- | --- | --- |
| Lake server | `v0.9.0-dogfood.fe787a4` | 176 MiB to about 2.82 GiB | 4.32 GiB |
| Capture agent | `v0.8.0`, `f150332a55c8` | about 1.51–1.65 GiB | 2.37 GiB |

RSS is resident process memory. These peaks come from `/proc/PID/status`, not
from systemd's service memory totals. Neither process restarted while sampled.
At the first observation the machine had about 9.2 GiB available out of 15 GiB,
with 691 MiB of swap occupied. Swap occupancy alone does not show active swapping.

During one 15-second sample, server RSS fell from 2.76 GiB to 176 MiB without
intervention. CPU consumption in that interval averaged 44.5% of one core for
the lake and 5.9% for the agent. This supports substantial temporary allocation
and eventual reclamation; it does not prove there are no retained objects or
leaks, especially in the agent.

A separate 45-second sample, with RSS read every five seconds, found:

- Lake RSS varied between 1.59 GiB and 2.82 GiB; agent RSS stayed around
  1.55–1.56 GiB.
- Lake CPU increased by 67.49 seconds: about 150% CPU, or 1.5 cores on average.
  Agent CPU increased by 3.21 seconds: about 7.1% CPU.
- Blob request-body bytes increased by 146,785 bytes, about 143 KiB.
- Five normalization jobs completed and four manifest requests completed.
  Summed normalization duration increased by 39.72 seconds. Jobs already in
  flight and other agents can contribute; these deltas do not identify a
  particular upload as the cause of all CPU use.

The agent journal's numeric watch fields showed growing Claude transcripts up
to 368,812,026 bytes (351.7 MiB), and Codex transcripts up to 123,168,212 bytes
(117.5 MiB). Last sync counters repeatedly showed one to three uploaded blobs
while approximately 250 sessions were unchanged. These are observations of
files being watched, not measurements of their retained heap size. Transcript
contents and identities were not needed for the analysis.

Service memory totals also include filesystem cache. In the 15-second sample,
the lake's cgroup contained about 2.4–2.7 GiB of file-backed cache, almost all
inactive, alongside the changing anonymous allocation. The agent's cgroup had
about 566 MiB of file cache. File cache is reclaimable and should be reported
separately from process RSS and live Go heap. A service memory limit would count
both, which makes a limit chosen from RSS alone misleading.

## What the code explains

The relevant upload, adapter, normalization and API source is unchanged between
`v0.8.0` and this checkout. Search code has changed, so agent and server builds
should still be identified separately in future measurements.

1. **Agent reads whole changed artifacts before uploading a tail.**
   `readArtifacts` in [prepare.go](../internal/upload/prepare.go) calls
   `os.ReadFile` for each artifact and checks its full digest. `cleanPrefix`
   hashes the previous prefix as well. `prepare` accumulates prepared sessions;
   their `full` maps keep complete bodies until the sync finishes. Tail body
   slices can also retain a complete backing allocation. Chunk splitting in
   [upload.go](../internal/upload/upload.go) can copy chunks. Network-efficient
   uploads therefore still perform work and hold memory proportional to the
   current transcript, or to multiple changed transcripts in one pass.
2. **The lake projects the whole current session again.**
   [Project](../internal/api/project.go) calls `CAS.Read`, which uses
   `io.ReadAll`, then a normalizer returning `[]Event`. Small tail uploads save
   network and CAS storage, but projection still reads and parses the assembled
   transcript. [worker.go](../internal/api/worker.go) runs two normalization
   workers, allowing two large jobs to overlap.
3. **Normalization and export create more representations.**
   The [Claude normalizer](../internal/normalize/claude.go) unmarshals nested JSON
   into maps of raw messages and decoded values. It retains an event slice.
   [Parquet export](../internal/normalize/parquet.go) then groups `ParquetRow`
   values for the full session, including a JSON serialization of every event,
   before writing. Writer column buffers and compression state add to the peak.
4. **Search already avoids unchanged database writes, but still reads events.**
   [indexSession](../internal/recall/index.go) streams the normalized session,
   parses rows and compares signatures, even when most rows have not changed.
   It should be profiled alongside normalization before attributing all lake
   CPU to either path.

Existing useful optimizations should be preserved: metadata memoization skips
opening unchanged files; tail secret scanning scans only new bytes plus its
boundary context; tail uploads avoid resending the whole file; search keeps
unchanged index rows. The upload memo holds digests and parsed identity metadata,
not complete raw transcript bodies. Compression already uses one encoder worker
in the relevant CAS, event and Parquet codecs. Reducing compression concurrency
is therefore not the obvious first fix.

## Isolated synthetic measurement

A scratch-only Go program called the checkout's real normalizer and writers.
It generated 128 MiB of valid Claude JSONL, consisting of 32,776 assistant text
records, then ran `Claude.Normalize`, `WriteEventsFile(io.Discard, events)` and
`WriteParquet` sequentially. Text was repetitive synthetic prose, about 3.8 KiB
per record; it contained no live transcript data. Go was 1.27.1, with
`GOMAXPROCS=12`. CPU profiling and heap sampling at 64 KiB were enabled. Raw
input became eligible for collection after normalization. The generated event
slice remained needed through export.

The baseline allocated approximately 784 MiB during normalization, 46 MiB during
compressed event output and 446 MiB during Parquet output. Including input
construction and profiling, total allocation before final collection was
1,406 MiB. This is allocation over time, not simultaneous live memory.

A heap allocation profile after collection attributed approximately 56% of
sampled bytes cumulatively to `Claude.Normalize`, and 31% to `WriteParquet`.
Nested JSON raw-message copies were the largest individual allocation site.
The CPU sample was dominated by JSON string parsing/quoting and memory copying;
compression was a smaller contributor on this repetitive input.

Separate process runs compared Go's soft memory limit:

| `GOMEMLIMIT` | Elapsed | Sampled peak RSS | GC cycles at end of Parquet | Total allocated before final GC |
| --- | --- | --- | --- | --- |
| unset | 2.25 s | 598.5 MiB | 7 | 1,406 MiB |
| 512 MiB | 2.27 s | 511.9 MiB | 15 | 1,461 MiB |
| 256 MiB | 5.34 s | not sampled | 443 | 2,521 MiB |

The 256 MiB run reported 354.5 MiB allocated heap at the Parquet phase boundary,
exceeding the soft limit. Repeated GC can also discard reusable pool entries,
which is consistent with the higher allocation total; this was not separately
isolated. A soft limit cannot make the required live working set disappear.

These are individual exploratory runs, not statistically stable performance
claims. They omit CAS assembly, live session diversity, simultaneous workers,
upload preparation and search indexing. They establish memory amplification in
the real projection/export functions; they are not heap profiles of the live
services. The 512 MiB result is a candidate for further isolated testing, not a
recommended production limit based on this one input.

After explicit GC, the baseline had about 24 MiB of heap left; after an explicit
release plus another GC it had about 1.8 MiB. This supports transient processing
and pooled state as contributors in the reproduction. Explicit GC/release was
used only in the scratch program, never against a live service.

## Recommended order

1. **Measure each live stage.** Add optional local diagnostics for both agent and
   lake, plus lightweight Go heap, allocation, GC and process CPU metrics. The
   current metrics endpoint reports application activity, but no Go heap or CPU
   profile. Capture an idle baseline and a growing large session. Measure RSS,
   cgroup anonymous memory, file cache, allocation rate, live heap after GC,
   queue age and stage duration separately. Include hashing/scanning, CAS
   reads, normalization, Parquet and indexing. Keep diagnostic access private.
2. **Bound agent preparation.** Process one session or a byte-bounded batch at a
   time. Use stable file-backed snapshots/chunks for retryable uploads, with
   bounded hashing/scanning buffers rather than full artifact byte slices.
   Preserve digest verification, same-size rewrite detection, secret detection
   across boundaries and the full-file fallback when a tail is refused. Tail
   slices alone do not bound memory if they retain the original allocation.
   TKT-01M44DRPS (Cursor CLI: stream the export so large stores need not be
   capped) already covers the Cursor export-specific problem.
3. **Stream lake projection and Parquet.** Let normalizers consume readers and
   emit events incrementally. Feed bounded Parquet batches rather than retaining
   all events and all serialized Parquet rows. Preserve generation checks and
   atomic publication: a partly built generation must not replace the last
   complete one. Limit simultaneous large jobs by estimated bytes, rather than
   relying only on a fixed worker count. Streaming primarily addresses memory;
   parsing the whole session still costs CPU.
4. **Reduce repeated CPU work.** Evaluate durable incremental parsing checkpoints
   and incremental derived output for append-only transcripts, with a full
   rebuild for rewrites/truncations. Preserve parser state, event IDs, content
   references, sidecars and generation semantics. Coalesce obsolete pending
   generations. The current worker skips already obsolete jobs at start, but
   two generations can still overlap after a newer append arrives. Indexing
   should reuse validated unchanged prefixes where possible.
5. **Evaluate operational knobs after the baseline.** A larger debounce window
   can reduce processing frequency at the cost of delayed capture/search.
   Defaults are 5 seconds quiet time and 30 seconds maximum wait; an isolated
   comparison such as 15/60 seconds would quantify the tradeoff. One normalize
   worker or a byte-weighted concurrency limit can reduce simultaneous peak
   memory at the cost of queue latency; worker count is currently a constant.
   Test moderate `GOMEMLIMIT` values against representative combined workload
   before changing the service. A low hard service limit risks killing work;
   lowering `GOGC` or the soft limit can increase CPU substantially.

Compression-level changes, dropping Parquet, changing capture permissions and
restarting services were not used as shortcuts. Compression and derived-store
choices need measured CPU, storage and product-use tradeoffs; the current
evidence puts whole-file processing and serialization ahead of those changes.

## Collection and reproduction notes

Read-only live inputs were `ps`, `free`, `/proc/PID/status` and `stat`,
`/proc/PID/cgroup`, cgroup `memory.stat`, systemd memory/CPU counters, aggregate
agent journal counters and `http://127.0.0.1:9187/metrics`. Journal messages were
parsed into numeric aggregates instead of being copied into the report. The
server account's heap and detailed mappings were not accessible, and the
application has no existing pprof handler. Exact live allocation attribution
remains open.

The scratch experiment lives at
`~/.cache/agent-scratch/lampi/memory-YCzu3i/` and is disposable. To recreate it,
use a module below the `terva.sh/lampi` import prefix, require the local checkout
through a `replace` directive, generate the synthetic JSONL described above, and
call the three public projection/export functions in that order. Record
`runtime.MemStats` at each boundary, use `runtime/pprof` for CPU and heap profiles,
and read `/proc/PID/status` while the child runs to sample its RSS high-water
mark. Run each memory-limit setting in a separate process. Keep input and output
under a task-specific agent scratch directory, and do not invoke live `sync`,
`serve` or operator maintenance commands for this reproduction.
