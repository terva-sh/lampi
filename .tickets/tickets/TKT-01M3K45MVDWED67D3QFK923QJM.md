---
schema: 3
id: TKT-01M3K45MVDWED67D3QFK923QJM
title: "CAS: store objects zstd-compressed; compact re-encodes"
type: task
status: ready
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
claim: null
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
