---
schema: 3
id: TKT-01M3558D7M09TNZYEJ961WC4AK
title: Decide lake host (NAS / VPS / S3+index VM)
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area/ops
  - phase/0-policy
  - question
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

Honest unknown from architecture: where the session lake runs. Affects encryption story, auth exposure, and daemon packaging.

## Notes

**human:sothr** at 2026-09-22T18:15:12Z

Options: home NAS, VPS, or S3-compatible object store + small index VM.
