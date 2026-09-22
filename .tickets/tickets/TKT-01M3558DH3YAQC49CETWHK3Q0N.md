---
schema: 3
id: TKT-01M3558DH3YAQC49CETWHK3Q0N
title: Append-only prefix merge on manifests (Layer B)
type: task
status: ready
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
updated_at: 2026-09-22T18:15:12Z
created_by:
  id: human:sothr
  name: Drew Short
updated_by:
  id: human:sothr
  name: Drew Short
extensions: {}
---

## Description

Manifest handling: no-op if full hash matches; accept tail if client is strict extension of server; stale client advances watermark; else conflict path.

## Acceptance criteria

- [ ] Append line → only tail uploaded
- [ ] Re-sync unchanged → zero new blobs
