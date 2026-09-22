---
schema: 3
id: TKT-01M3558DEJYSZ08P53KM9CNVSY
title: One-shot sync uses outbox + watermarks end-to-end
type: task
status: ready
status_reason: null
priority: high
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
  - TKT-01M3558DBS1J4V1VB5C0FVNWKP
  - TKT-01M3558DCDJ71TN19N5DDY4RSF
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

Promote scaffold `sync` from bare CAS upload to full pipeline (redact, watermark, manifest ACK). Re-sync must upload zero new blobs for unchanged files.
