---
schema: 3
id: TKT-01M3JV45Y7RJ7S7Y3WWW2WMEKC
title: "Lake storage: sample disk use by component and record growth"
type: task
status: in-progress
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
claim:
  actor: agent:claude-code/e4a47e8c
  branch: ops/storage-samples
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-e4a47e8c
  commit: 8fea3abdf6076b070ce78f94ae51a7130695ec9b
  session: null
  claimed_at: 2026-09-28T01:47:35Z
  expires_at: null
archive: null
created_at: 2026-09-28T01:47:29Z
updated_at: 2026-09-28T01:47:35Z
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

- [ ] Serve samples each component's bytes and files at start and on an interval
- [ ] Samples are stored in the catalog by migration 10 and thinned past a bound
- [ ] Filesystem total and free bytes and referenced bytes are recorded
- [ ] The walk never follows symlinks and never runs on a request
