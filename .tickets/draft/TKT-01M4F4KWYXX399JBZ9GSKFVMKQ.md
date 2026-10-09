---
schema: 3
id: TKT-01M4F4KWYXX399JBZ9GSKFVMKQ
title: "CAS: compact stored blobs while the lake stays online"
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/cas
  - area/server
  - area/ops
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-09T01:32:05Z
updated_at: 2026-10-09T01:32:05Z
created_by:
  id: agent:codex/t3code-8afe4a1e
  name: ""
updated_by:
  id: agent:codex/t3code-8afe4a1e
  name: ""
extensions: {}
---

## Description

Assessment requested by the operator after the d7dcde1 dogfooding rollout. This ticket records the source assessment and proposed implementation, not authorization to implement it or run maintenance on the live lake. Implementation remains draft.

### Conclusion and scope

Online stored-blob compaction is feasible using the existing content-addressed objects, compressed frames, chunk lists, and prefix records. No blob-format or capture-protocol change appears necessary. The hard work is coordinating reference publication and physical deletion with readers, uploads, and backups. It is a substantial storage concurrency change, not removal of the CLI lake lock.

The target should be that the server remains available and captures continue uploading, with bounded delays during publication and deletion. Compaction can still consume disk bandwidth and CPU; zero latency impact is not a realistic promise. Search-index optimization stays a separate maintenance action.

### Evidence from current code

Assessed source at main 10d86676f16f3879162102ac5a7c4d03a354a9af (production code d7dcde1).

- `internal/cli/compact.go:79`: a mutating compact takes lake.lock and opens a separate idle Server. A dry run takes no lake lock. The process lock prevents simultaneous owners, and should remain in place.
- `internal/api/compact.go:75`: Compact enumerates sessions, plans and hash-verifies version folds, applies folds, traverses live digests, sweeps unreferenced entries, and re-encodes remaining raw objects. Its loops are designed around a stationary store. Session listing, artifact reads, and referenced-digest reads are separate catalog operations, not one consistent snapshot.
- `internal/api/compact.go:428` and `internal/api/live.go:23`: the keep set comes from catalog digests, last manifests, and their transitive chunk/prefix dependencies. Entries are selected and later removed without an atomic recheck against new catalog references or active work. MinAge is a useful retention policy, not a synchronization mechanism.
- `internal/cas/compact.go:46`, `internal/cas/prefix.go:344`, and `internal/cas/logical.go:199`: Fold/Grow publish durable logical records before removing superseded objects, using the store mutex and atomic rename. Grow and FoldGrowth already run during ingest. These are useful foundations; their existence does not make the entire offline compact pipeline concurrency-safe.
- `internal/cas/cas.go:215` and `internal/cas/logical.go:242`: readers do not hold the writer mutex. logicalReader captures a chunk list and opens one chunk at a time. A digest root pin alone is insufficient after that list has been replaced: the reader still needs dependencies in the old representation.
- `internal/cas/zstd.go:316`: Reencode streams compression outside the mutex, then publishes a frame and removes the raw representation. It is explicitly intended for an offline compactor; the online implementation must revalidate representation state and reader-open handoffs before publication/removal.
- `internal/api/server.go:581,681`: blob PUT and manifest POST are separate requests. Manifest resolution checks/assembles blobs before the catalog transaction commits. The heavy-request semaphore limits concurrency, but does not exclude a compactor or represent pending blob references.
- `internal/cli/lakeops.go:154`, `internal/cli/archive.go:73`, and `internal/cas/maint.go:362`: directory and encrypted-archive backups take a catalog snapshot and copy CAS from another process, with no shared in-process mutex. Directory backup closes missing prefix/chunk dependencies after walking. New GC can invalidate those old copied records unless backups participate in retention.
- `internal/web/maintenance.go` and `internal/cli/serve.go:716`: the dashboard already has authenticated, audited, asynchronous single-job coordination and shutdown cancellation. It can host an online worker after storage invariants are established. Current job timeout is one hour and state is in memory.

### Concrete failure cases to prevent

1. The collector marks an old uploaded blob unreferenced. A manifest then commits a reference to it; the collector deletes its precomputed candidate. The catalog now names missing bytes. Reusing an old digest may not change its mtime, so increasing MinAge does not close this race.
2. A reader opens a logical file and reads its first chunk. Compaction replaces the list with a prefix record and sweeps the old chunks. The reader fails when it reaches a later chunk even though the file's current representation is valid.
3. A backup has already copied a logical record naming an intermediate version. Flattening makes that intermediate unreachable in the current lake and GC removes it before backup dependency closure can copy it. Current catalog reachability alone does not protect the backup.
4. An upload, tail assembly, or chunk binding has not yet published catalog roots. GC must not remove inputs or intermediate outputs while that operation is active. Between PUT and manifest, recently acknowledged/reused digests need a retention lease or a documented retry contract.
5. A fold or re-encode computed outside the writer lock reaches publication after another operation changed the representation. Its hash proof remains about immutable bytes, but the dependency graph, cycle check, and deletion decision need validation against current state.

### Recommended design

Run an online compactor inside the existing serve process using its actual Catalog and CAS instances. Keep lake.lock held by serve; do not launch the offline CLI in parallel. A future CLI may submit a job to the server through an authenticated local/admin route, rather than independently mutating its store.

Introduce explicit retention and mutation coordination:

- A read lease protects the dependency representation a reader captured, through Close. It must retain old chunk lists/prefix paths and their required entries when records are replaced, not merely re-traverse the root's latest graph. Acquire the lease consistently with resolving/opening the representation. Audit all CAS consumers, including normalization and exports.
- Upload/assembly leases protect inputs and outputs before catalog publication, including deduplicated objects and logical dependencies. Transfer protection to committed roots atomically with respect to deletion. Retain acknowledged uploads for a grace period between requests; delayed manifests must either renew protection or get a safe missing-blob response before committing. Crash/restart handling must conservatively retain potentially pending work until leases can be reconstructed or a recovery grace interval expires.
- Build a consistent catalog-root snapshot, then track additions to roots and dependency edges while marking. A generation/epoch plus a mutation journal or equivalent barrier is one candidate. Publication paths must register relevant changes before they can race deletion. Deletion checks the candidate's representation/version and retention state atomically; a changed candidate is skipped or retried. A generation counter alone is not sufficient unless all reference mutations participate and the check and removal share the barrier.
- Hold synchronization only while acquiring retention or publishing/revalidating a bounded change. Hashing, compression, full root traversal, network-body reads, and disk streaming should stay outside global publication locks. Establish a documented catalog/CAS/retention lock order. Existing BindLogical and keepFrame also perform hashing under the store mutex and need a lock-duration audit before promising bounded upload delays.
- Start with cross-process coordination that defers destructive blob compaction for the whole lifetime of directory/archive backups, including catalog snapshot and CAS closure. Readers and ingestion stay available. A shared/exclusive maintenance file lock is an option only if every relevant backup path participates and platform semantics are tested. Later, snapshot-specific backup leases could allow both jobs to reclaim concurrently. Account for backups run with an older binary that cannot participate: deployment compatibility must prevent such overlap; a new lock alone cannot coordinate old tools.
- For malformed live logical metadata or an incomplete root traversal, fail closed and skip collection. Deferred deletion is preferable to risking sessions. Keep purge/fsck-repair under their existing exclusive offline contract in the first version.

Make work incremental and cancellable: process bounded batches, replan changed graphs, persist only the state required for correctness, and report actual reclaimed bytes, skipped/pinned entries, current phase, and reason for waiting. Check cancellation during hashing, traversal, and sweep; do not interrupt a durable publication halfway. A crash may leave both representations or retained garbage, but must leave at least one valid representation and a safe rerun. Avoid adding a persistent queue/schema migration unless required after the retention design is proven.

### Delivery stages and verification

1. Establish retention and publication invariants, instrument lock duration, and add deterministic interleaving tests before enabling a live worker.
2. Enable online folds, flattening, and legacy re-encoding behind an explicit opt-in, with orphan sweeping disabled until its marking/deletion proof and backup coordination pass. Representation removal still needs read/backup protection; this is not safe merely because the sweep is disabled.
3. Enable bounded orphan collection, then add an Operations action using existing fresh-admin authentication, CSRF protection, audit records, job exclusion, and shutdown handling. Separate estimate/run requests and expose useful progress. Consider periodic low-budget scheduling only after manual dogfooding shows bounded resource impact and retry behavior.

Essential tests include an old deduplicated blob becoming referenced between mark and sweep; delayed/multi-request and resumed uploads; tail assembly and shared chunks; slow readers spanning list replacement/flattening; multiple sessions sharing digests; concurrent graph changes that would introduce a cycle; raw/frame publication and repair; cancellation and forced termination at durability boundaries; disk-full/read-only/I/O failures; directory and encrypted-archive backup restore plus fsck; and restart with pending upload retention. Run Go race tests, but use explicit barriers to make filesystem/reference races deterministic: the race detector alone cannot prove these invariants.

Benchmark on a synthetic lake shaped like dogfooding, with large chunked files and long version chains: upload ACK p95/p99, normalization/read latency, peak extra disk, CPU, memory used by roots/leases, and reclamation throughput. Current assessment is source review only; it establishes no latency or throughput measurements and does not read or mutate protected lake data.

### Alternatives and effort

- Removing lake.lock or calling the existing Compact from a background goroutine loses the offline safety premise and leaves the failure cases above open.
- Holding one mutex through the entire run might avoid some races only if every caller participates, but blocks capture for the duration and does not protect existing lazy readers or external backups. It does not meet the intended availability goal.
- A longer age threshold or two scans separated by a delay improves retention heuristics but cannot prove safety when references change or an old digest is reused. An epoch scheme works only with coordinated reference publication and reader retention.
- An append-only storage generation with atomic cutover and delayed generation retirement is a viable alternative. It provides snapshot retention more naturally but requires broader layout/tool changes and potentially much more temporary disk. Packing blobs into segments is not required for the current request.

Recommend retaining the existing representation and delivering the concurrency protections in several reviewable changes. This is a medium-to-large engineering effort dominated by GC/reader/backup correctness and testing; the Operations integration is comparatively small. A defensible schedule needs a retention prototype and workload benchmark. No release date, schema change, or zero-impact guarantee is established by this assessment.

## Acceptance criteria

- [ ] The lake stays available for reads and capture uploads during bounded blob compaction.
- [ ] Reference publication, active readers and pending uploads cannot race physical deletion.
- [ ] Directory and archive backups remain restorable during compaction, with older-tool compatibility addressed.
- [ ] Cancellation, restart and injected I/O failures preserve readable catalog-referenced bytes.
- [ ] Operations exposes authenticated, audited requests and progress after concurrency and workload gates pass.
