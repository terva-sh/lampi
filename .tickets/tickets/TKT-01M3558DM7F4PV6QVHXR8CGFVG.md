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

Project terva raw blobs into schema_version 1 normalized events; export for DuckDB/SQLite proof query of a known prompt.

## Acceptance criteria

- [ ] Unknown harness fields preserved
- [ ] opaque encrypted_content stays opaque
- [ ] Known prompt substring queryable after ingest

## Definition of done

- [ ] Tickets 030–032 done
