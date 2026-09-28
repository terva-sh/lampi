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
updated_at: 2026-09-28T23:50:26Z
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
