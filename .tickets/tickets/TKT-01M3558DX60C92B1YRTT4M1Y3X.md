---
schema: 3
id: TKT-01M3558DX60C92B1YRTT4M1Y3X
title: Project linking via normalized git remote
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/catalog
  - phase/2-harness
assignees: []
milestone: phase-2
parent: TKT-01M3558DV5NHYZBFMPV1AVRNYQ
origin: null
dependencies:
  - TKT-01M3558DJXS57VSP73457Z1DGD
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-22T18:15:12Z
updated_at: 2026-09-23T12:27:00Z
created_by:
  id: human:sothr
  name: Drew Short
updated_by:
  id: agent:cursor/e4d5
  name: Cursor cloud agent
extensions: {}
---

## Description

Layer C project_id from git_remote_normalized (+ root commit); stop relying on path CWDHash across machines.

## Acceptance criteria

- [ ] project_id comes from git_remote_normalized and the root commit
- [ ] The same repository on two machines links without using path CWDHash
