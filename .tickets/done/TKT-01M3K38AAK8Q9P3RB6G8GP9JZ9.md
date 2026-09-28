---
schema: 3
id: TKT-01M3K38AAK8Q9P3RB6G8GP9JZ9
title: "Lake storage grows quadratically: every grown transcript version is a full copy"
type: bug
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area/cas
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
created_at: 2026-09-28T04:09:33Z
updated_at: 2026-09-28T05:27:30Z
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

## Implementation plan

Store a superseded version as a prefix record, `{prefix_of, length}`,
under cas/logical/ beside the chunk lists, instead of as a whole copy.

- **Ingest:** `Store.Grow` assembles the stored head and the tail in one
  streamed pass. It hashes both the prefix and the whole, installs the
  grown object, then writes the head's record and removes its object.
  This replaces the hash-then-copy pair, which read the head twice.
- **Reads:** Open, Read and Size follow record chains, and chunk lists
  open their parts through the store. Each link must be strictly
  longer, and a visited set stops loops. A base that ends early is
  ErrUnexpectedEOF, never a short read. Nesting is capped at 8.
- **Stored or not:** `Present` (an object, or a record that resolves)
  answers blobs/check and the manifest's missing check, and Put
  discards a body whose digest is a record that reads. A client that
  re-sends an old version therefore does not store a second copy.
- **fsck:** reports a record whose chain does not resolve. Repair keeps
  it, because a put of the digest restores an object and Open prefers
  the object.
- **Backup:** compares logical/ entries by content. Records are
  re-pointed at the same size.
- **Purge:** the keep set follows chains transitively. A kept record
  whose base belongs to the purged session is written out whole
  (`Materialize`) before the base is removed, so a purge still removes
  the bytes after the shared prefix.
- **Left to `serve compact`:** folding existing whole copies, flattening
  chains, and dropping tails. Tails are not deleted at ingest because
  another artifact may name the same bytes, and only a whole-lake view
  under the lake lock can tell.

## Notes

**agent:claude-code/e4a47e8c** at 2026-09-28T04:18:02Z

Diagnostics, 2026-09-27. They were run without access to the live
catalog, which needs sudo.

- **The hosted lake:** 37.6 GiB of stored blobs in 9,154 files, and
  65.7 GiB of logical bytes across 4,691 artifact rows. These figures
  come from the new operations page.
- **What it should hold:** the agent's watermarks give the current size
  of every file this machine, the lake's only contributor, has
  uploaded. That is 193 files and 0.53 GiB: 461.5 MiB claude, 78.2 MiB
  codex, 0.1 MiB terva. The largest is 59.6 MiB, and 3 files exceed
  the 32 MiB blob cap. So the lake stores about 70 times its current
  data.
- **A reproduction on a test lake.** One file was grown through tail
  uploads with the real handler, then the CAS was measured with
  `storage.Measure`. The stored-to-final ratio is appends/2 + 1:

  | appends | final size | CAS size  | files | ratio  |
  |---------|------------|-----------|-------|--------|
  | 20      | 1.0 MiB    | 11.4 MiB  | 39    | 11.7×  |
  | 100     | 4.9 MiB    | 252.7 MiB | 199   | 51.7×  |
  | 200     | 9.8 MiB    | 994.6 MiB | 399   | 101.6× |

  The file count is one tail and one assembled copy per append, which
  matches `TestAppendTailAndUnchangedResync`.
- **Files over the 32 MiB cap** are sent whole as fixed 32 MiB chunks.
  Their leading chunks are shared between versions, so they grow by at
  most one chunk per version. That is linear but still wasteful.

Nothing reads a superseded version's bytes on a hot path. Normalize
reads the head, checkClient reads the current prefix, and excerpts read
normalized JSONL. The old bytes are kept only as history, and every one
of them is recoverable as a prefix of its successor.

The reproduction test is kept at /var/tmp/ops/growth_diag_test.go on
the dev host, so the fix can adopt it as its regression test.

**agent:claude-code/e4a47e8c** at 2026-09-28T04:37:56Z

### Alternatives rejected

- **Delete tails at ingest.** A tail digest can equal another
  artifact's whole bytes, or a tail another session is about to
  reference. Deciding that needs the catalog and every logical index.
  Compact does it under the lake lock.
- **Rewrite every record in a chain on each append, keeping chains
  flat.** That costs O(n) writes per append. Reads of old versions are
  rare, and compact flattens. Ingest keeps one write per append.
- **Keep `Has` false for records and let clients re-upload.** That
  self-heals but stores a second full copy each time. The purge test
  exposed this, since a second session posts v1 whole. `Present` is
  used instead.
- **Keep a purged session's newest file alive because another session's
  record reads from it.** That defeats purge, which exists to remove
  leaked text. The purge test's leaked line survived. `Materialize` is
  used instead.

### Measured

The same diagnostic as before, one file grown by tails, shows the CAS
at 2.3×, 2.3× and 2.2× the final file after 20, 100 and 200 appends,
against 11×, 51× and 101× before. What remains is the head plus the
tails, which is compact's job.

### Rollback

A binary older than this one reads a prefix record as a malformed
logical index. After this ships, rolling back needs compact's inverse,
which does not exist. Treat this as forward-only.

## Summary

Landed in #54. A grown version is now stored as a prefix record of its
successor, `{prefix_of, length}` under cas/logical/, instead of as a
whole copy.

- Ingest's `Store.Grow` streams the head and the tail once.
- Reads follow record chains, and fall back to the record when Grow
  removes an object mid-read.
- Present, Size and fsck check that each chain resolves, that every
  link is longer than the one before, and that the file at the end
  holds the last link's length.
- Backup follows every record and chunk list in the copy until it
  resolves.
- Purge writes out whole any shared version whose bytes lie in the
  purged session's files.

The diagnostic now shows 2.2× the final file after 200 appends,
against 101× before. The remaining 1.2× is the tails, which
`serve compact` (TKT-01M3K45MS) removes, along with the whole copies
lakes already hold.

Review took five rounds on the v0.3.0 and v0.5.0 reviewers. Ten
findings were fixed and one rejected: a retried tail manifest does
reach Grow, which is tested. The format is forward-only, because an
older binary cannot read a prefix record.
