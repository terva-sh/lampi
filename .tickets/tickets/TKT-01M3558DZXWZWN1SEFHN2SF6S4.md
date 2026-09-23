---
schema: 3
id: TKT-01M3558DZXWZWN1SEFHN2SF6S4
title: OpenCode via scheduled opencode export
type: task
status: ready
status_reason: null
priority: low
due_on: null
labels:
  - area/adapter
  - phase/3-export
assignees: []
milestone: phase-3
parent: TKT-01M3558DZ6CBW2HB49WFFY20CP
origin: null
dependencies:
  - TKT-01M3558DV5NHYZBFMPV1AVRNYQ
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-22T18:15:12Z
updated_at: 2026-09-23T17:30:15Z
created_by:
  id: human:sothr
  name: Drew Short
updated_by:
  id: agent:cursor/c937
  name: Cursor cloud agent
extensions: {}
---

## Description

Prefer `opencode export` / db path discovery over live SQLite WAL tails.

## Acceptance criteria

- [ ] Ingest prefers a scheduled `opencode export` or a discovered database path
- [ ] Live SQLite WAL tails are not the read path
