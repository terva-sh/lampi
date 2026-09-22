---
schema: 3
id: TKT-01M3558DJXS57VSP73457Z1DGD
title: "Catalog: session_uid, aliases, provenance rows"
type: task
status: done
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
updated_at: 2026-09-22T22:07:47Z
created_by:
  id: human:sothr
  name: Drew Short
updated_by:
  id: agent:cursor/b3f1
  name: Cursor cloud agent
extensions: {}
---

## Description

Assign session_uid (ULID) once; aliases map (harness, native_id, machine_id); provenance rows for multi-machine CAS hits.

## Acceptance criteria

- [x] session_uid is assigned once for a harness, native id, and machine id
- [x] An alias maps that triple back to the uid
- [x] A second machine recording the same bytes adds provenance and no new blob

## Implementation plan

Keep one session_uid per (harness, native_session_id). Aliases map (harness, native_id, machine_id) to that uid. Provenance rows are (session_uid, machine_id, sha256). A second machine posting the same digest adds an alias and a provenance row and no CAS object.

## Summary

session_uid is assigned once per harness and native id. aliases maps harness, native id, and machine id to that uid. A second machine posting the same digest adds a provenance row and no blob.
