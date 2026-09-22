---
schema: 3
id: TKT-01M3558DFTQRSAQGK8FK2864YT
title: POST /v1/hello (protocol versions, server_time, max_blob)
type: task
status: ready
status_reason: null
priority: high
due_on: null
labels:
  - area/protocol
  - area/server
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

Device-token hello returns server_time, supported protocol versions, max_blob_bytes. Client warns on clock skew.

## Acceptance criteria

- [ ] The hello response includes server_time, supported protocol versions, and max_blob_bytes
- [ ] The client warns when its clock skews from server_time
