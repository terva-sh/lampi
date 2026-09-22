---
schema: 3
id: TKT-01M3558DNF9KDSAGCB5FFA7JSM
title: Export normalized JSONL/Parquet for DuckDB
type: task
status: done
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
updated_at: 2026-09-22T23:42:25Z
created_by:
  id: human:sothr
  name: Drew Short
updated_by:
  id: agent:cursor/66b0
  name: Cursor cloud agent
extensions: {}
---

## Description

Synchronous MVP export job producing files DuckDB/sqlite3 can query for content_text.

## Acceptance criteria

- [x] Export writes normalized JSONL or Parquet
- [x] DuckDB or sqlite3 can query content_text from that export

## Implementation plan

terva-lampi export reads the lake and writes one JSON object per normalized event. Sessions whose normalize_error is set are skipped and named on stderr. The file is newline-delimited JSON with a content_text field, which is what sqlite json_extract and DuckDB read_ndjson both query. The test loads the export into sqlite and selects content_text.

## Summary

terva-lampi export writes one JSON object per normalized event. Sessions with normalize_error set are skipped and named on stderr. The test loads that JSONL into sqlite and selects json_extract(line, '$.content_text'). DuckDB can read the same file with read_ndjson.
