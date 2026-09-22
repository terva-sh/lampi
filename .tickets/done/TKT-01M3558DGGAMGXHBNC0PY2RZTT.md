---
schema: 3
id: TKT-01M3558DGGAMGXHBNC0PY2RZTT
title: Harden POST /v1/blobs/check + resumable PUT
type: task
status: done
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
updated_at: 2026-09-22T22:53:47Z
created_by:
  id: human:sothr
  name: Drew Short
updated_by:
  id: agent:cursor/19a7
  name: Cursor cloud agent
extensions: {}
---

## Description

Batch missing-digest check; PUT with Content-Range or chunk digests; assemble when complete. Keep Layer A idempotent put.

## Acceptance criteria

- [x] blobs/check names the digests the lake is missing
- [x] PUT resumes by Content-Range or chunk digests and finishes when they assemble
- [x] Putting a digest the lake already has stores nothing

## Implementation plan

POST /v1/blobs/check already returns the digests the lake does not have. Keep that, and cover a mixed batch in a test.

PUT /v1/blobs/{sha256} gains two resume forms on top of the idempotent whole-object put. Content-Range: bytes start-end/total writes that slice under cas/partial and installs the object only when the ranges cover the total and the hash matches the path. A JSON body {"chunk_sha256s":[...]} concatenates those CAS objects in order and installs the same way. A digest that is already stored returns exists:true and writes nothing, including for a range or a chunk list. A manifest that lists chunk_sha256s assembles the same way once every chunk is present, then Layer B runs on the full bytes.

The client upload path sends Content-Range pieces when Options.PieceBytes is set, and chunk digests when Options.ChunkBytes is set. The default sync put stays one body.

## Summary

POST /v1/blobs/check returns the digests the lake does not have, once each. PUT accepts a whole body, a Content-Range slice, or a JSON chunk_sha256s list, and installs the object when the pieces assemble and hash to the path. A digest that is already stored returns exists true and does not rewrite the blob. A manifest chunk list assembles the same way, then Layer B runs on the full bytes. upload.Sync sends ranges when PieceBytes is set and chunk digests when ChunkBytes is set. The default sync put is still one body.
