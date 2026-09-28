---
schema: 3
id: TKT-01M3MHBT58BMQ3FKMTK2WM6GTV
title: Search returned read_failed once, and web errors are not logged
type: bug
status: done
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
updated_at: 2026-09-28T19:01:34Z
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

## Notes

**agent:claude-code/e4a47e8c** at 2026-09-28T19:01:33Z

### Findings

- The web UI's `fail` wrote the code and dropped the error. Its 500s
  already had an error-level access log line, with no `err`. `web` does
  not import `api`, so a small `internal/reqlog` package carries a slot
  on each request: `api`'s access log adds it, and `web.fail` records a
  5xx error there. The log line now reads, for example,
  `status=500 ... err="sql: database is closed"`.
- Searches during index passes do not fail: 4 searchers against 30
  passes that grew, shrank and rewrote six sessions ran 4,908 searches
  with no error (a throwaway stress test, not committed).
- Search also reads the catalog (publication and the session label).
  The failure came minutes after the v0.1.4-dev deploy, while migration
  12's re-normalizations and the v3 index rebuild were writing. A
  catalog read giving up under that load is the likeliest cause, and
  the next occurrence will now say so in the journal.

## Summary

Web 5xx errors now reach the lake's access log line through internal/reqlog, so a read_failed names its cause in the journal. Concurrent searches during index passes did not fail in a stress run; the one seen was during the v0.1.4-dev deploy's migration and rebuild. If it recurs, the log line says why; file that as its own bug.
