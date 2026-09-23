---
schema: 3
id: TKT-01M3558E2PNBKNHFY6J7A37GJ4
title: Phase 4 Cursor (explicitly late)
type: epic
status: ready
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

Version-pinned SQLite snapshot readers for Cursor IDE and CLI. Never ingest cursorAuth/*. Expect breakage across major versions; confidence=low.

## Definition of done

- [ ] All children of this epic are done, or wontfix with a rationale
- [ ] TKT-01M3558E3ER1TS33KZB0AG7XX4 Version-pinned Cursor state.vscdb snapshot reader
- [ ] TKT-01M3558E45YCTAZET208YAAQFV Cursor CLI store.db adapter (separate corpus)

## Notes

**agent:cursor/e85b** at 2026-09-23T21:01:42Z

Promoted with two children. TKT-01M3558E3ER1TS33KZB0AG7XX4 (Version-pinned Cursor state.vscdb snapshot reader) and TKT-01M3558E45YCTAZET208YAAQFV (Cursor CLI store.db adapter) gained acceptance criteria taken from their descriptions. The store.db adapter depends on the IDE reader and stays a separate corpus.

TKT-01M3558E21PZDJEYAAR4WH9B85 (Soft-link git-ticket claims to lake session_uid) stays draft. Phase 5 (TKT-01M3558E4TN25NXXB0XP127RVK) stays draft with its children.
