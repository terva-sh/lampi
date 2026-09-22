---
schema: 3
id: TKT-01M3558DMTAXM5GGN2QW0C728R
title: Normalize terva raw → schema_version 1 events
type: task
status: ready
status_reason: null
priority: high
due_on: null
labels:
  - area/normalize
  - phase/1-mvp
assignees: []
milestone: mvp
parent: TKT-01M3558DM7F4PV6QVHXR8CGFVG
origin: null
dependencies:
  - TKT-01M3558DJXS57VSP73457Z1DGD
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-22T18:15:11Z
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

Fill normalize stub: raw blob → normalized events per research schema §5.1. Failures leave raw intact; mark normalize_error.
