---
schema: 3
id: TKT-01M3558DM7F4PV6QVHXR8CGFVG
title: MVP normalize + search proof
type: epic
status: ready
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

Project terva raw blobs into schema_version 1 normalized events; export for DuckDB/SQLite proof query of a known prompt.

## Acceptance criteria

- [ ] Unknown harness fields preserved
- [ ] opaque encrypted_content stays opaque
- [ ] Known prompt substring queryable after ingest

## Definition of done

- [ ] All children of this epic are done
- [ ] TKT-01M3558DMTAXM5GGN2QW0C728R Normalize terva raw → schema_version 1 events
- [ ] TKT-01M3558DNF9KDSAGCB5FFA7JSM Export normalized JSONL/Parquet for DuckDB
- [ ] TKT-01M3558DQJV7479AMV66QYM8B0 Acceptance: query known prompt substring
