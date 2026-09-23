---
schema: 3
id: TKT-01M3558E0M4CT3DWR4Z4GEY584
title: "Optional sidecars: terva raati/ and tasks archives"
type: task
status: ready
status_reason: null
priority: low
due_on: null
labels:
  - area/adapter
  - phase/3-export
assignees: []
milestone: phase-3
parent: TKT-01M3558DZ6CBW2HB49WFFY20CP
origin: null
dependencies:
  - TKT-01M3558DPVCN661600P3F9HMB0
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-22T18:15:12Z
updated_at: 2026-09-23T17:30:15Z
created_by:
  id: human:sothr
  name: Drew Short
updated_by:
  id: agent:cursor/c937
  name: Cursor cloud agent
extensions: {}
---

## Description

Optional artifact families under TERVA_HOME beyond transcripts.

## Acceptance criteria

- [ ] raati/ under TERVA_HOME is an optional artifact family beyond transcripts
- [ ] tasks archives under TERVA_HOME are an optional artifact family beyond transcripts
- [ ] Transcript ingest still succeeds when neither family is present
