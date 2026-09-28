---
schema: 3
id: TKT-01M3JV45Y7RJ7S7Y3WWW2WMEKC
title: "Lake storage: sample disk use by component and record growth"
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/ops
  - area/catalog
  - area/server
assignees: []
milestone: null
parent: TKT-01M3JV3TRWB6JQQGY1F84JCFDE
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-28T01:47:29Z
updated_at: 2026-09-28T02:03:17Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/e4a47e8c
  name: ""
extensions: {}
---

## Description

The lake's disk use is the sum of several directories under the data
directory: the CAS, the catalog, normalized projections, parquet
partitions, the search index and the audit log. Today nobody can see how
big each one is, or how fast it grows, without running `du` on the host.

Sample the size of each component in the serve process: once at start,
then on an interval. Record the samples in the catalog so the size can be
charted over time. Record the filesystem's total and free bytes with each
sample, and the bytes the catalog references, so the page can show how
much deduplication saves.

### Approach

- Migration 10 adds `storage_samples`: one row per sample and component,
  holding bytes and files. The filesystem's total and free bytes are
  stored as pseudo-components.
- A sampler walks the data directory and sorts every file into one of
  these components: cas, catalog, normalized, parquet, search, audit, or
  other. It never follows symlinks.
- The walk runs off the request path, on a timer. A slow disk delays the
  next sample; it never delays a request.
- Retention: samples older than a bound are thinned, keeping one per day.

## Acceptance criteria

- [x] Serve samples each component's bytes and files at start and on an interval
- [x] Samples are stored in the catalog by migration 10 and thinned past a bound
- [x] Filesystem total and free bytes and referenced bytes are recorded
- [x] The walk never follows symlinks and never runs on a request

## Implementation plan

- `internal/storage` has two entry points:
  - `Measure(ctx, dir)` walks the lake directory with WalkDir, which does
    not follow symlinks, and sorts each regular file into one component
    by its path.
  - `Capacity(dir)` asks the filesystem for its total and free bytes.
    It uses statfs on unix and GetDiskFreeSpaceEx on windows, and
    reports ErrUnsupported on any other platform.
- Bytes are allocated blocks (st_blocks*512) on unix and file size on
  other platforms.
- Catalog migration 10 adds `storage_samples(sampled_ns, measure, bytes,
  files)`. The measures are the components plus `fs.total`, `fs.free`,
  `artifacts.referenced` and `artifacts.unique`.
- `RecordStorage` thins rows older than 14 days to the last sample of
  each UTC day, inside the same transaction as the insert.
- `api.Server.SampleStorage` combines the walk, the capacity and
  `ArtifactBytes`, and records the result.
- `serve` runs it at start and then every hour, in a goroutine. It stops
  that goroutine from BeforeClose, chained ahead of the web index's
  hook, so no sample writes after the catalog closes.

## Notes

**agent:claude-code/e4a47e8c** at 2026-09-28T01:52:15Z

Decisions, with the alternatives each one beat:

- **Samples live in the catalog, not in a sidecar file or memory.**
  Memory loses the history at every restart, and growth over time is
  the point. A sidecar would be one more file for backup to know about.
  The catalog is already backed up and already migrated.
- **Sample hourly, not every few minutes.** Each sample walks every
  file. Disk growth that matters shows up over hours, and an hourly
  cadence with 14 days of detail keeps the table at a few thousand rows.
- **Record allocated blocks, not file sizes.** The question is how much
  disk the lake takes, and that matches `du`. File size is kept only
  where the platform reports no block count.
- **Classify files by path, with a catch-all "other" component.** A new
  file type then lands in "other" instead of vanishing, so the
  components always add up to the whole directory.
- **Export `cas.TempFile` instead of copying the prefix list.** An
  install temp file then counts as uploads in both places without the
  two lists drifting apart.

## Summary

Landed in PR #49. Serve now measures the lake directory's disk use:

- at start, then hourly, off the request path
- by component: cas, uploads, catalog, normalized, parquet, search,
  audit, other
- as allocated blocks, including directories; a symlinked lake root is
  resolved, and nothing inside the lake is followed

With each sample it records the filesystem's total and free bytes, and
the referenced and unique artifact bytes, read from one snapshot.
Samples live in `storage_samples` (catalog migration 10). Those older
than 14 days are thinned to one per UTC day.

Review rounds fixed:

- the symlinked root
- directory blocks
- the Windows free-space path separator
- the paired artifact queries
