---
schema: 3
id: TKT-01M3558DM7F4PV6QVHXR8CGFVG
title: MVP normalize + search proof
type: epic
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area/normalize
  - area/search
  - phase/1-mvp
assignees: []
milestone: mvp
parent: null
origin: null
dependencies: []
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

Project terva raw blobs into schema_version 1 normalized events; export for DuckDB/SQLite proof query of a known prompt.

## Acceptance criteria

- [x] Unknown harness fields preserved
- [x] opaque encrypted_content stays opaque
- [x] Known prompt substring queryable after ingest

## Definition of done

- [x] All children of this epic are done
- [x] TKT-01M3558DMTAXM5GGN2QW0C728R Normalize terva raw → schema_version 1 events
- [x] TKT-01M3558DNF9KDSAGCB5FFA7JSM Export normalized JSONL/Parquet for DuckDB
- [x] TKT-01M3558DQJV7479AMV66QYM8B0 Acceptance: query known prompt substring

## Implementation plan

The three children cover the projection, the JSONL export, and the known-prompt query. This epic closes when those three are done. Unknown fields and opaque encrypted_content are asserted on the projection; the prompt query is the export test.

## Summary

terva raw projects to schema_version 1 events, unknown fields and opaque encrypted_content are kept, and terva-lampi export writes JSONL that sqlite can query for a fixture prompt.

Children TKT-01M3558DMTAXM5GGN2QW0C728R, TKT-01M3558DNF9KDSAGCB5FFA7JSM, and TKT-01M3558DQJV7479AMV66QYM8B0 are done.
