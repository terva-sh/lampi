---
schema: 3
id: TKT-01M3558DBS1J4V1VB5C0FVNWKP
title: Per-path watermark store
type: task
status: ready
status_reason: null
priority: urgent
due_on: null
labels:
  - area/agent
  - phase/1-mvp
assignees: []
milestone: mvp
parent: TKT-01M3558D9Z1ZWHXB3V14A4AJGR
origin: null
dependencies:
  - TKT-01M3558DB2BJEEYN3G8M5GAJ17
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

Watermarks keyed by (machine_id, harness, root_path, relative_path): last uploaded size, mtime, content sha256, byte-offset for append-only.

## Acceptance criteria

- [ ] Updated only after manifest ACK
- [ ] Drives tail-only uploads
