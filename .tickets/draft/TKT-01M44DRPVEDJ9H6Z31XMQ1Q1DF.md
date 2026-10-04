---
schema: 3
id: TKT-01M44DRPVEDJ9H6Z31XMQ1Q1DF
title: Cursor IDE normalize searches the whole export once per row
type: bug
status: draft
status_reason: null
priority: low
due_on: null
labels:
  - area/normalize
  - phase/4-cursor
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-04T21:40:21Z
updated_at: 2026-10-04T21:40:21Z
created_by:
  id: agent:claude-code/580cbe08
  name: ""
updated_by:
  id: agent:claude-code/580cbe08
  name: ""
extensions: {}
---

## Description

The Cursor IDE projector in `internal/normalize/cursor.go` gives each row its offset with `cursorOffset`, a `bytes.Index` over the whole export from the start, the same pattern TKT-01M44DDXWQ6MG51BMVF5FDCAEA (Cursor CLI normalize searches the whole export once per row) fixed for the CLI projector. A large workspace export would cost rows times size to normalize, and a row whose key another row names would get that row's offset. The CLI fix (per-row spans from one decoder pass) should carry over.

## Acceptance criteria

- [ ] Normalizing a Cursor IDE export reads it about once
- [ ] A row's offset points at that row
