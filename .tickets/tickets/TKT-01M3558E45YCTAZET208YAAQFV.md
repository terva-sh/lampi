---
schema: 3
id: TKT-01M3558E45YCTAZET208YAAQFV
title: Cursor CLI store.db adapter (separate corpus)
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
  - TKT-01M3558E3ER1TS33KZB0AG7XX4
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

Separate from IDE corpus; do not assume sync.

## Acceptance criteria

- [ ] The CLI store.db corpus is separate from the IDE state.vscdb corpus
- [ ] The adapter does not assume the CLI store is in sync with IDE state
- [ ] This ticket depends on the IDE state.vscdb reader
