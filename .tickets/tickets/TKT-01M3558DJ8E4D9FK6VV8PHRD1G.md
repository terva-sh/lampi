---
schema: 3
id: TKT-01M3558DJ8E4D9FK6VV8PHRD1G
title: Device-token auth hygiene (--token-file, hashed server-side)
type: task
status: ready
status_reason: null
priority: high
due_on: null
labels:
  - area/auth
  - phase/1-mvp
assignees: []
milestone: mvp
parent: TKT-01M3558DF4KG977332Q7NG30VB
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

Per-device 256-bit bearer tokens, hashed at rest on server, client reads --token-file only (never argv). Single-tenant, many devices.

## Acceptance criteria

- [ ] The server stores a hash of the device token
- [ ] The client reads the token only from --token-file
- [ ] Each device has its own token
