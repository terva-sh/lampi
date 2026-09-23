---
schema: 3
id: TKT-01M3558E3ER1TS33KZB0AG7XX4
title: Version-pinned Cursor state.vscdb snapshot reader
type: task
status: ready
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
updated_at: 2026-09-23T21:01:42Z
created_by:
  id: human:sothr
  name: Drew Short
updated_by:
  id: agent:cursor/e85b
  name: Cursor cloud agent
extensions: {}
---

## Description

Read-only snapshot of IDE global/workspace DBs. confidence=low. Never ingest cursorAuth/*. Copy WAL trio for consistency.

## Acceptance criteria

- [ ] Read-only snapshot of the IDE global state.vscdb and each workspace state.vscdb
- [ ] The reader is confidence=low and version-pinned
- [ ] Keys under cursorAuth/* are never ingested
- [ ] The WAL trio (state.vscdb and present state.vscdb-wal and state.vscdb-shm) is copied for a consistent snapshot before the database is opened
- [ ] A major Cursor upgrade may break the pin, and the pin is documented
