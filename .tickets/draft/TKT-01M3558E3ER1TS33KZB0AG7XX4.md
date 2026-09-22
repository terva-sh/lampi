---
schema: 3
id: TKT-01M3558E3ER1TS33KZB0AG7XX4
title: Version-pinned Cursor state.vscdb snapshot reader
type: task
status: draft
status_reason: null
priority: low
due_on: null
labels:
  - area/adapter
  - phase/4-cursor
assignees: []
milestone: phase-4
parent: TKT-01M3558E2PNBKNHFY6J7A37GJ4
origin: null
dependencies:
  - TKT-01M3558DZ6CBW2HB49WFFY20CP
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-22T18:15:12Z
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

Read-only snapshot of IDE global/workspace DBs. confidence=low. Never ingest cursorAuth/*. Copy WAL trio for consistency.
