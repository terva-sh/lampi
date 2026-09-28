---
schema: 3
id: TKT-01M3KC2DDVJ0QBPKES9A2YD2WF
title: Search index grows by a third after re-normalizing every session
type: bug
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/search
assignees: []
milestone: null
parent: TKT-01M3K45MQBRESGG5HEZZSZ3FDQ
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-28T06:43:37Z
updated_at: 2026-09-28T06:43:37Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/e4a47e8c
  name: ""
extensions: {}
---

## Description

The search index went from 485.6 MiB to 659 MiB after
`serve normalize --all` re-projected all 92 sessions on 2026-09-28,
although the sessions did not change.

Suspected cause: each session's rows are deleted and reinserted into
the trigram FTS5 table, and deleted rows are dropped only when FTS5
segments merge. The file does not shrink without an `optimize` and a
`VACUUM`.

To do:

- Measure the same pass on a copy of an index.
- Check the effect of `INSERT INTO fts(fts) VALUES('optimize')` followed
  by `VACUUM`.
- Decide where compaction belongs: after an index pass that rewrote
  many sessions, or as part of `serve compact`.

This is separate from the size of a fresh index, which TKT-01M3K45MX
covers.
