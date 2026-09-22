---
schema: 3
id: TKT-01M3558DKNVT2CQKQCZ756Y4K5
title: Document wire format in docs/protocol.md
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/docs
  - area/protocol
  - phase/1-mvp
assignees: []
milestone: mvp
parent: TKT-01M3558DF4KG977332Q7NG30VB
origin: null
dependencies:
  - TKT-01M3558DFTQRSAQGK8FK2864YT
  - TKT-01M3558DH3YAQC49CETWHK3Q0N
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

capture_protocol: 1 — hello, blobs/check, blobs PUT, manifests JSON schema, watermark ACK semantics.

## Acceptance criteria

- [x] docs/protocol.md covers hello, blobs/check, blob PUT, and manifests for capture protocol 1
- [x] Watermark ACK semantics in that doc match the server

## Implementation plan

Update docs/protocol.md so the manifest section matches Layer B: tail wire fields, grown_from, stale watermark, divergent_copy, aliases, and provenance. Leave chunks and pull described as not implemented.

## Summary

docs/protocol.md matches capture protocol 1 as implemented: hello, blob check, blob PUT, manifests, and the ACK fields head_size and relation, including the stale watermark advance. Chunks and pull stay documented as not implemented.
