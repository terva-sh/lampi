---
schema: 3
id: TKT-01M3558E5QS1JN6B7HDATCCC2C
title: Allowlisted trajectory / ShareGPT export
type: task
status: ready
status_reason: null
priority: low
due_on: null
labels:
  - area/normalize
  - phase/5-train
assignees: []
milestone: phase-5
parent: TKT-01M3558E4TN25NXXB0XP127RVK
origin: null
dependencies:
  - TKT-01M3558D8AVEAA1565Y6KRNJTE
  - TKT-01M3558D8WN5HTVPM4KRQQCSHP
  - TKT-01M3558DMTAXM5GGN2QW0C728R
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-22T18:15:12Z
updated_at: 2026-09-23T22:21:09Z
created_by:
  id: human:sothr
  name: Drew Short
updated_by:
  id: agent:cursor/cbf6
  name: Cursor cloud agent
extensions: {}
---

## Description

Export filtered datasets with raw_sha256 lineage; keep opaque encrypted_content opaque.

## Acceptance criteria

- [ ] Export is an allowlisted trajectory / ShareGPT dataset
- [ ] The export keeps raw_sha256 lineage
- [ ] Opaque encrypted_content stays opaque
