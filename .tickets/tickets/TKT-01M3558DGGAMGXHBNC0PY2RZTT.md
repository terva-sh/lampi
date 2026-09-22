---
schema: 3
id: TKT-01M3558DGGAMGXHBNC0PY2RZTT
title: Harden POST /v1/blobs/check + resumable PUT
type: task
status: ready
status_reason: null
priority: high
due_on: null
labels:
  - area/protocol
  - area/cas
  - phase/1-mvp
assignees: []
milestone: mvp
parent: TKT-01M3558DF4KG977332Q7NG30VB
origin: null
dependencies: []
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

Batch missing-digest check; PUT with Content-Range or chunk digests; assemble when complete. Keep Layer A idempotent put.
