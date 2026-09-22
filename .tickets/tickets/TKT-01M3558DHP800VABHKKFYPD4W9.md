---
schema: 3
id: TKT-01M3558DHP800VABHKKFYPD4W9
title: divergent_copy conflict artifacts + catalog fields
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

When prefix check fails for same logical session, store client blob as new artifact; link under session_uid with relation=divergent_copy; never silent merge.

## Acceptance criteria

- [ ] A failed prefix check stores the client bytes as a new artifact
- [ ] The catalog links that artifact to the session_uid as divergent_copy
- [ ] The two copies are not merged
