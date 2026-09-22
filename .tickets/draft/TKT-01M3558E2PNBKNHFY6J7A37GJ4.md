---
schema: 3
id: TKT-01M3558E2PNBKNHFY6J7A37GJ4
title: Phase 4 Cursor (explicitly late)
type: epic
status: draft
status_reason: null
priority: low
due_on: null
labels:
  - area/adapter
  - phase/4-cursor
assignees: []
milestone: phase-4
parent: null
origin: null
dependencies:
  - TKT-01M3558DZ6CBW2HB49WFFY20CP
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-22T18:15:12Z
updated_at: 2026-09-22T18:31:21Z
created_by:
  id: human:sothr
  name: Drew Short
updated_by:
  id: human:sothr
  name: Drew Short
extensions: {}
---

## Description

Version-pinned SQLite snapshot readers for Cursor IDE and CLI. Never ingest cursorAuth/*. Expect breakage across major versions; confidence=low.

## Definition of done

- [ ] All children of this epic are done, or wontfix with a rationale
- [ ] TKT-01M3558E3ER1TS33KZB0AG7XX4 Version-pinned Cursor state.vscdb snapshot reader
- [ ] TKT-01M3558E45YCTAZET208YAAQFV Cursor CLI store.db adapter (separate corpus)
