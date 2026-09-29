---
schema: 3
id: TKT-01M3K45MVDWED67D3QFK923QJM
title: "CAS: store objects zstd-compressed; compact re-encodes"
type: task
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area/cas
  - area/server
assignees: []
milestone: null
parent: TKT-01M3K45MQBRESGG5HEZZSZ3FDQ
origin: null
dependencies:
  - TKT-01M3K45MSX5SG7YZZCR399S9XF
blocks_on: none
references: []
claim:
  actor: agent:claude-code/d8436f9f
  branch: t3code/check-zstd-compression-tickets
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-d8436f9f
  commit: f78ca7b7f6cef29c22255d334798d05814021dc6
  session: null
  claimed_at: 2026-09-28T23:50:26Z
  expires_at: null
archive: null
created_at: 2026-09-28T04:25:34Z
updated_at: 2026-09-29T00:07:39Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/d8436f9f
  name: ""
extensions: {}
---

## Description

Store CAS objects compressed with zstd at the better level. That gives
5.4× on the hosted lake's real transcripts; see the epic for the
measurements.

- The object key stays the digest of the uncompressed bytes.
- Readers decompress transparently, and Size reports the logical size.
- Existing uncompressed objects keep working.
- `serve compact` re-encodes objects, and each chain's newest version
  as one frame, avoiding the independent-frame penalty (3.3× instead of
  5.0×).
- Backup copies the stored form. Fsck verifies the decompressed hash.

## Implementation plan

Two PRs.

1. **Compressed at rest.** Every install writes a zstd frame at
   `sha256/<ab>/<rest>.zst`. Readers, Size, Has, Present, fsck, repair,
   backup and the entry walk all handle both forms. Raw objects keep
   working.
2. **Compact re-encodes.** `serve compact` rewrites each raw object as a
   frame and reports the bytes it saved. A grown version is already one
   whole frame, because Grow writes the grown file whole.

## Notes

**agent:claude-code/e4a47e8c** at 2026-09-28T05:27:30Z

### Design notes (before implementing)

- **Where it goes.** Every install goes through `commitFileLocked(digest,
  src, size)` with a synced raw file: Put, PutRange, Concat, Grow and
  Materialize. Compression belongs there, done before the store lock is
  taken so a 32 MiB encode does not serialize uploads.
- **Naming.** Store compressed objects as `sha256/ab/<rest>.zst` rather
  than sniffing the zstd magic. Tails and chunks can begin at any byte of
  a file, so content sniffing could misread a raw object. Has then checks
  two names.
- **Size.** Write the frame with its content size
  (`Encoder.ResetContentSize`), so Size reads a frame header instead of
  decompressing.
- **OpenBlob.** It returns `*os.File`, and callers stat it for the size:
  installChunks, BindLogical, Concat and Size. Those need a logical-size
  call instead.
- **Normalized JSONL.** The recall reader pages with byte-offset cursors
  (`recall/events.go`, `cursor.Off`), so compressing that file needs a
  seekable framing or a cursor change. That belongs to TKT-01M3K45MX.

**agent:claude-code/d8436f9f** at 2026-09-29T00:07:39Z

### Compressed at rest: built (first PR)

- **Seal.** Each install writes its raw temp file as it did before. `seal`
  then compresses it into a synced `.put-*` temp beside the object, and
  `commitFileLocked` renames that onto `<rest>.zst` and removes any raw
  file under the same digest. Put, Grow, Materialize and GrowParts seal
  before taking `s.mu`. Concat and the range-upload install already held
  it and still do.
- **Frame header.** klauspost's streaming encoder records the content
  size only from 256 bytes up, and writes nothing at all for empty
  input. Measured with a probe on v1.17.9.
  - `logicalSize` reads the header when it holds the size, and otherwise
    decodes and counts, which is at most 255 bytes.
  - The empty object is a fixed 9-byte single-segment frame
    (`emptyFrame`).
- **Pools.** Decoders and encoders are pooled. Allocating a decoder on
  each open of a 32 MiB chunk cost about 9 MB of window each time. This
  showed up in `TestUnchangedRepostDoesNotReadTheFile`.
- **BindLogical.** It now checks for the same recorded index before it
  hashes the chunks. An unchanged re-post of a file past the cap used to
  re-read the whole file. The test only passed because raw reads used
  32 KB buffers.
- **Sizes.** `ObjectSize` is the logical size. The new `StoredSize` is disk
  bytes, and Fold's `freed`, compact's dry-run count and `Reclaimed` use
  it. `ObjectSize` treats an object removed between the lookup and the
  open as gone, so a head read during Grow's supersede falls through to
  the record, as it did before.
- **Weaker size check.** Has and Present check sizes, not bytes. A frame
  cut short still records its whole size, so truncation of a compressed
  object shows only on read or fsck. Every install syncs before the
  rename, so a crash leaves an empty file, which Has still reports
  missing.
- **Backup.** Frames are copied as stored. A raw file with a `.zst`
  beside it is skipped, and a raw copy in the destination is removed
  once the frame is there.
- **Repair.** A damaged raw file beside an intact frame is removed on its
  own.
- **Rollback.** An older release cannot read `.zst` objects.
  `vps-bringup.md` and `container.md` say to back up before this upgrade.

### Alternatives

- **Sniff the zstd magic instead of a suffix.** Rejected, as the earlier
  note says: a raw chunk or tail can begin with those four bytes.
- **Compress while streaming the body, one pass instead of two.**
  Rejected: the frame could not record its size, since Put does not know
  it up front, and Size would have to decode.
- **Keep small objects raw when compression does not help.** Rejected: it
  leaves both forms live for good, and compact could not tell a raw
  object it has not reached from one it chose to leave.
- **Split into a read-only PR and a writing PR,** so a rollback across
  that boundary is safe. Not done: the two ship in the same release
  unless the owner releases between them. The PR says so.
