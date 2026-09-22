---
schema: 3
id: TKT-01M3558DFTQRSAQGK8FK2864YT
title: POST /v1/hello (protocol versions, server_time, max_blob)
type: task
status: done
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
updated_at: 2026-09-22T22:07:47Z
created_by:
  id: human:sothr
  name: Drew Short
updated_by:
  id: agent:cursor/b3f1
  name: Cursor cloud agent
extensions: {}
---

## Description

Device-token hello returns server_time, supported protocol versions, max_blob_bytes. Client warns on clock skew.

## Acceptance criteria

- [x] The hello response includes server_time, supported protocol versions, and max_blob_bytes
- [x] The client warns when its clock skews from server_time

## Implementation plan

Hello already returns server_time, protocol_versions, and max_blob_bytes. Add a client clock check against server_time (warn past 5m) on Result.Warning, printed by sync and agent, with Now injectable for tests.

## Summary

POST /v1/hello returns server_time, protocol_versions, and max_blob_bytes behind the device token. upload.Sync warns on Result.Warning when the client clock is more than five minutes from server_time; sync and agent print that on stderr and still upload.
