---
schema: 3
id: TKT-01M3558DJXS57VSP73457Z1DGD
title: "Catalog: session_uid, aliases, provenance rows"
type: task
status: ready
status_reason: null
priority: high
due_on: null
labels:
  - area/catalog
  - phase/1-mvp
assignees: []
milestone: mvp
parent: TKT-01M3558DF4KG977332Q7NG30VB
origin: null
dependencies:
  - TKT-01M3558DH3YAQC49CETWHK3Q0N
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-22T18:15:11Z
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

Assign session_uid (ULID) once; aliases map (harness, native_id, machine_id); provenance rows for multi-machine CAS hits.

## Acceptance criteria

- [ ] session_uid is assigned once for a harness, native id, and machine id
- [ ] An alias maps that triple back to the uid
- [ ] A second machine recording the same bytes adds provenance and no new blob
