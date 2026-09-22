---
schema: 3
id: TKT-01M3558DH3YAQC49CETWHK3Q0N
title: Append-only prefix merge on manifests (Layer B)
type: task
status: done
status_reason: null
priority: urgent
due_on: null
labels:
  - area/protocol
  - area/catalog
  - phase/1-mvp
assignees: []
milestone: mvp
parent: TKT-01M3558DF4KG977332Q7NG30VB
origin: null
dependencies:
  - TKT-01M3558DFTQRSAQGK8FK2864YT
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-22T18:15:11Z
updated_at: 2026-09-22T22:07:47Z
created_by:
  id: human:sothr
  name: Drew Short
updated_by:
  id: agent:cursor/b3f1
  name: Cursor cloud agent
extensions: {}
---

## Description

Manifest handling: no-op if full hash matches; accept tail if client is strict extension of server; stale client advances watermark; else conflict path.

## Acceptance criteria

- [x] Append line → only tail uploaded
- [x] Re-sync unchanged → zero new blobs

## Implementation plan

When Plan is KindTail, PUT only the suffix and send honest prev/tail_sha256/full sha256. The manifest handler assembles prefix||tail, verifies the full hash, and moves head. Equal full hash is a no-op. A strict server-side prefix of the client is stale: keep head and return head_size so the client watermark advances.

## Summary

KindTail PUTs only the new suffix, with byte_watermark_prev and tail_sha256 set to that suffix and sha256 set to the full file. The lake assembles the tail, stores the full digest, and moves the head. An equal digest stores nothing. A stale prefix keeps the head and the client watermark advances to head_size.
