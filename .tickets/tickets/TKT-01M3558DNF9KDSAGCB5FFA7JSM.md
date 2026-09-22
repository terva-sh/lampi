---
schema: 3
id: TKT-01M3558DNF9KDSAGCB5FFA7JSM
title: Export normalized JSONL/Parquet for DuckDB
type: task
status: ready
status_reason: null
priority: high
due_on: null
labels:
  - area/search
  - phase/1-mvp
assignees: []
milestone: mvp
parent: TKT-01M3558DM7F4PV6QVHXR8CGFVG
origin: null
dependencies:
  - TKT-01M3558DMTAXM5GGN2QW0C728R
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

Synchronous MVP export job producing files DuckDB/sqlite3 can query for content_text.

## Acceptance criteria

- [ ] Export writes normalized JSONL or Parquet
- [ ] DuckDB or sqlite3 can query content_text from that export
