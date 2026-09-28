---
schema: 3
id: TKT-01M3MHBT58BMQ3FKMTK2WM6GTV
title: Search returned read_failed once, and web errors are not logged
type: bug
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/search
  - area/ops
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-28T17:35:22Z
updated_at: 2026-09-28T17:35:22Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/e4a47e8c
  name: ""
extensions: {}
---

## Description

On 2026-09-28, shortly after the 0.1.4-dev (d8a0b50) deploy, a search
for `TKT-01M396R1CX` filtered to `event_type=message` returned
`read_failed`, a 500. The same URL worked on a retry. The index was
rebuilding (version 3) and the migration's re-normalizations were
running at the time.

The cause cannot be found. `web.fail` maps an unrecognised error to
`read_failed` and writes no log line with the error, so a transient
SQLite error, a bug, and a closed database look the same.

### To do

- Log every 5xx from the web handlers with the underlying error, at
  warn, as the API's access log does with `note`.
- Then check whether search reads can fail while a pass holds the
  index's write transaction or runs `incremental_vacuum`.
- Locally, the same query over six large sessions takes 3 ms and plans
  from the FTS index, so it is not the 5 s read timeout, which would
  have been `read_unavailable`.
