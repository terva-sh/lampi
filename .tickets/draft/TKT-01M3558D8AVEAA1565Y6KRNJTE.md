---
schema: 3
id: TKT-01M3558D8AVEAA1565Y6KRNJTE
title: Write retention + encryption-at-rest policy
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area/ops
  - phase/0-policy
  - policy
assignees: []
milestone: null
parent: TKT-01M3558D72YST7VYN39EFK272P
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-22T18:15:11Z
updated_at: 2026-09-22T18:15:11Z
created_by:
  id: human:sothr
  name: Drew Short
updated_by:
  id: human:sothr
  name: Drew Short
extensions: {}
---

## Description

Document TTL, encryption at rest (age/LUKS/S3 SSE), and whether raw may leave the machine for all vs allowlisted projects.

## Acceptance criteria

- [ ] Policy markdown under docs/
- [ ] Defaults safe for single-tenant Drew machines
