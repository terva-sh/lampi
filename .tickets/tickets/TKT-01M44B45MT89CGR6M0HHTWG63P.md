---
schema: 3
id: TKT-01M44B45MT89CGR6M0HHTWG63P
title: "Cursor CLI: upload store.db blobs as content-addressed objects"
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/adapter
  - area/protocol
  - area/cas
  - area/normalize
  - phase/4-cursor
assignees: []
milestone: null
parent: null
origin: null
dependencies:
  - TKT-01M44B45JRVQ1W2XF5H0K3MC4X
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-04T20:54:11Z
updated_at: 2026-10-04T20:54:17Z
created_by:
  id: agent:claude-code/580cbe08
  name: ""
updated_by:
  id: agent:claude-code/580cbe08
  name: ""
extensions: {}
---

## Description

A Cursor CLI session is uploaded as one JSON document that holds every row of `store.db`. The document is rebuilt and uploaded whole on every change: the artifact's `byte_watermark_prev` is always 0. Blobs are written in id order, and a blob id is a content hash, so a new blob lands at a random place in the document and shifts every fixed 4 MiB upload piece after it. A change costs close to the whole session in upload and in lake storage.

ACP sessions on one workstation run to 744M and 2.5G. At the agent's 30-second debounce cap, one active session would re-upload hundreds of megabytes a minute and store each version.

A blob id is the hex SHA-256 of the blob bytes (2,000 of 2,000 checked on a live ACP store). So the blobs can go to the lake as their own content-addressed objects and the session document can list them by id, so a change uploads only the new blobs and the stored versions share the old ones. This changes what the lake stores for `cursor-cli` and what the normalize projector reads, so it needs a design that the lake, an older agent and an older lake can all live with.

## Acceptance criteria

- [ ] A change to a Cursor CLI session uploads only blobs the lake does not already hold
- [ ] The lake can still read, normalize and export sessions stored in the whole-document form
- [ ] An older lake, or a newer lake with an older agent, keeps working
- [ ] The redaction scan still covers every blob that is uploaded
