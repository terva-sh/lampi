---
schema: 3
id: TKT-01M3558DCDJ71TN19N5DDY4RSF
title: Redaction ruleset v1 + quarantine on hits
type: task
status: ready
status_reason: null
priority: urgent
due_on: null
labels:
  - area/redact
  - phase/1-mvp
assignees: []
milestone: mvp
parent: TKT-01M3558D9Z1ZWHXB3V14A4AJGR
origin: null
dependencies:
  - TKT-01M3558D8WN5HTVPM4KRQQCSHP
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

Local secret regex scan before network. Stamp redaction.ruleset=v1 on artifact metadata. Quarantine hits; allowlisted projects only for raw off-box.

## Acceptance criteria

- [ ] Common token/key patterns caught in fixtures
- [ ] Hits never uploaded without explicit override path
