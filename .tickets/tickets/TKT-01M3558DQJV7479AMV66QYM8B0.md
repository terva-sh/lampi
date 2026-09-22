---
schema: 3
id: TKT-01M3558DQJV7479AMV66QYM8B0
title: "Acceptance: query known prompt substring"
type: task
status: ready
status_reason: null
priority: high
due_on: null
labels:
  - area/search
  - area/ci
  - phase/1-mvp
assignees: []
milestone: mvp
parent: TKT-01M3558DM7F4PV6QVHXR8CGFVG
origin: null
dependencies:
  - TKT-01M3558DNF9KDSAGCB5FFA7JSM
  - TKT-01M3558DPVCN661600P3F9HMB0
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-22T18:15:11Z
updated_at: 2026-09-22T18:31:22Z
created_by:
  id: human:sothr
  name: Drew Short
updated_by:
  id: human:sothr
  name: Drew Short
extensions: {}
---

## Description

Architecture §7 test #5 — after ingest, query normalized text for a fixture prompt.

## Acceptance criteria

- [ ] After ingest, a query finds a known fixture prompt in normalized text
