---
schema: 3
id: TKT-01M3K45MQBRESGG5HEZZSZ3FDQ
title: "Lake storage efficiency: smaller than the raw sessions it holds"
type: epic
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area/cas
  - area/server
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim:
  actor: agent:claude-code/e4a47e8c
  branch: tickets/lake-growth
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-e4a47e8c
  commit: 166beea48329836945572e4c735c94dfd8ad82a3
  session: null
  claimed_at: 2026-09-28T04:25:34Z
  expires_at: null
archive: null
created_at: 2026-09-28T04:25:34Z
updated_at: 2026-09-28T04:25:34Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/e4a47e8c
  name: ""
extensions: {}
---

## Description

Goal: the lake stores the sessions it holds in fewer bytes than the
harnesses keep them raw on the machines that produced them.

On 2026-09-27 the hosted lake held 0.53 GiB of current session data, as
544 MiB of raw files, but used 39.1 GiB of disk:

- 37.6 GiB of blobs, because every grown version was a full copy
  (TKT-01M3K38AA)
- 541 MiB of parquet, written uncompressed
- 486 MiB of search index, a trigram FTS5 table plus a copy of every
  event's text

Measured on those same 544 MiB of transcripts:

| method | size | ratio |
|---|---|---|
| zstd, default level | 107.9 MiB | 5.0× |
| zstd, better level | 100.1 MiB | 5.4× |
| zstd, best level | 96.6 MiB | 5.6× |
| gzip | 151.4 MiB | 3.6× |
| whole corpus as one stream | 89.2 MiB | 6.1× |
| per file with a trained 112 KiB dictionary | 98.9 MiB | 5.5× |
| independent 64 KiB frames, as raw appended tails | 166.2 MiB | 3.3× |

Cross-file redundancy therefore adds little, and a dictionary is not
worth its complexity. Small independent frames lose a third of the
gain, so heads should be re-encoded whole.

The work, in order:

1. prefix records for grown versions (TKT-01M3K38AA)
2. `serve compact`
3. zstd at rest
4. the footprint of the derived stores
