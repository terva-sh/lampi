---
schema: 3
id: TKT-01M3558DXV7FRPJ5A5HXMSW5B4
title: Async normalizer workers + parquet partitions
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/normalize
  - phase/2-harness
assignees: []
milestone: phase-2
parent: TKT-01M3558DV5NHYZBFMPV1AVRNYQ
origin: null
dependencies:
  - TKT-01M3558DMTAXM5GGN2QW0C728R
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

Move normalize off synchronous MVP path; partition parquet by date/harness.

## Acceptance criteria

- [ ] Normalize runs off the synchronous MVP manifest path
- [ ] Parquet is partitioned by date and harness
