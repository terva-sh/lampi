---
schema: 3
id: TKT-01M3KC2DAAZSVA0XQ23XAAXSSE
title: Transcripts past 32 MiB still grow quadratically between compactions
type: bug
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area/cas
  - area/agent
  - area/server
assignees: []
milestone: null
parent: TKT-01M3K45MQBRESGG5HEZZSZ3FDQ
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-28T06:43:36Z
updated_at: 2026-09-28T06:43:36Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/e4a47e8c
  name: ""
extensions: {}
---

## Description

Prefix records (TKT-01M3K38AA) fixed growth for files up to the 32 MiB
object cap. A transcript past the cap still grows quadratically between
compactions.

### Cause

The agent sends a tail only when the whole file fits in one object
(`upload/prepare.go:395`). A larger file is sent whole as 32 MiB chunks.
The first chunk dedups across versions. The last chunk (everything past
32 MiB) is new in each version, and the previous version's chunk list,
with its own last chunk, stays until `serve compact` folds it.

### Observed

On 2026-09-28 the hosted lake's blobs went from 1008.7 MiB to 1.39 GiB in
about 40 minutes. The cause was this session's 40.7 MiB transcript: each
sync stored about 8.7 MiB.

### Directions

- Let a chunk list's parts be any readable digest. A tail on a large
  file then records the new version as the previous version's parts plus
  the tail, which is O(1) per append. The previous version becomes a
  prefix record, as for small files. Many small parts accumulate, so
  compact would consolidate them into chunk-sized objects.
- Or content-defined chunk boundaries on the client, so only the last
  chunk changes and it stays small. That needs a protocol change on
  both sides.
- `serve compact` already reclaims the space, so running it
  periodically works around the problem.
