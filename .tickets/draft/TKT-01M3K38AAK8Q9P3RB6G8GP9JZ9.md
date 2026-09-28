---
schema: 3
id: TKT-01M3K38AAK8Q9P3RB6G8GP9JZ9
title: "Lake storage grows quadratically: every grown transcript version is a full copy"
type: bug
status: draft
status_reason: null
priority: high
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
claim: null
archive: null
created_at: 2026-09-28T04:09:33Z
updated_at: 2026-09-28T04:09:33Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/e4a47e8c
  name: ""
extensions: {}
---

## Description

The hosted lake holds 37.6 GiB of blobs for 91 sessions. That filled
the host's disk when a pre-upgrade backup tried to copy it (2026-09-27).

### Cause

A growing transcript is uploaded as a tail. `checkClient` in
`internal/api/merge.go` then assembles the stored prefix and the tail
into a new full object, with `CAS.Put(a.SHA256, prefix+tail)`. Every
earlier version stays in the CAS, and so does the tail blob.

So a transcript that grew n times to size S occupies roughly n·S/2.
Storage grows with the square of a session's length. Every superseded
version is a byte prefix of the version that replaced it (`grown_from`
links them), so its bytes are fully recoverable from the newer one and
a stored length. The tails are likewise redundant once assembled.

### Directions to weigh

1. **Stop materialising.** Record a grown version as a logical file:
   the prefix's chunks plus the tail, through `BindLogical`. Storage
   becomes linear. Chunk lists grow with every append, so they need
   periodic compaction. Readers, fsck, backup and purge must handle
   shared chunks.
2. **Keep only the newest version in each grown_from chain.** Represent
   older versions as "the first N bytes of D" and delete their objects
   and the assembled tails. This is linear too, needs no change to the
   upload path, and a one-time migration shrinks existing lakes.
   Normalisation of an old generation would read a truncated newer
   blob.
3. **Retention only.** Prune superseded versions past an age. This is
   the simplest, but it discards history rather than deduplicating it.

Measure first: how much of the store is superseded prefixes, tails, and
current heads.
