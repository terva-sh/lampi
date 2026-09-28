---
schema: 3
id: TKT-01M3KC2DAAZSVA0XQ23XAAXSSE
title: Transcripts past 32 MiB still grow quadratically between compactions
type: bug
status: in-progress
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
claim:
  actor: agent:claude-code/e4a47e8c
  branch: cas/large-file-tails
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-e4a47e8c
  commit: 4df2e01c039fe85adc89898bb8d9289cc036aba5
  session: null
  claimed_at: 2026-09-28T06:52:15Z
  expires_at: null
archive: null
created_at: 2026-09-28T06:43:36Z
updated_at: 2026-09-28T07:03:30Z
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

## Implementation plan

Fix the storage side on the lake, with no protocol change, so every
client already deployed benefits.

- `cas.Store.FoldGrowth(prev, next)`: when a chunked version replaces
  the version at its relpath, walk both piece lists in order (an object
  is a list of one). Shared chunks are already one object. For the
  first chunk that differs, if next's chunk is longer and its first
  bytes hash to prev's chunk, fold prev's chunk into a prefix record of
  it (`Fold`, which refuses loops). Stop at the first chunk that is not
  extended.
- Call it from `checkClient` after a chunked artifact's bytes are
  checked. A failed fold is logged and the ingest goes on; the new
  version is already stored, and `serve compact` reclaims what a fold
  missed.
- A chunk can now be a prefix record, so every place that read chunks
  as plain objects follows records: `BindLogical`, `Concat`, fsck's
  chunk check, `resolve`'s missing-chunk check, `installChunks`, and
  the chunk-list PUT's missing check.

## Notes

**agent:claude-code/e4a47e8c** at 2026-09-28T07:03:30Z

### Alternatives considered

- Let the client send a tail for files past the cap (the ticket's first
  direction), with the lake growing the last chunk. It also saves
  bandwidth, but it is a protocol change: a v0.1.3 lake answers an
  oversized tail with 400 (Grow rejects a prefix at the cap), not the
  409 that makes a client widen to the whole file, so the client would
  need a capability in hello. Deferred to a follow-up; the storage
  growth is what fills the disk.
- Fold the whole previous version into a record of the new one, as
  small files do. That leaves the old last chunk unreferenced until the
  compact sweep, so the bytes stay on disk between compactions, which
  is the bug.
- Content-defined chunking on the client. Needs both sides and still
  keeps one extra partial chunk per version without a fold.

### Cost

One hash of at most one chunk (32 MiB) per chunked ingest. Record
chains lengthen by one per sync until the file crosses the next chunk
boundary, as they already do for small files.
