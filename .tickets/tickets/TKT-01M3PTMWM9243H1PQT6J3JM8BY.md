---
schema: 3
id: TKT-01M3PTMWM9243H1PQT6J3JM8BY
title: "Conflicts: operator makes a divergent copy the session head"
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
  - area/catalog
assignees: []
milestone: null
parent: TKT-01M3PTMA4C1XD80THS0AXEKK1Y
origin: null
dependencies:
  - TKT-01M3PTMWHRS56CX6QZEV8EER13
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-29T14:56:05Z
updated_at: 2026-09-29T14:56:06Z
created_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
extensions: {}
---

## Description

When a harness rewrites a session file and keeps appending to it, every
post is another divergent copy and the session's head never moves. The
only way forward today is a purge. An operator should be able to say
"this copy is the session now".

### Approach

- "Make this the head" on a conflict page: the copy becomes current at
  its path and the session head; the row the head was at stops being
  current; the session is queued for normalization, as an ingest that
  moved the head would be; a `head_updates` row records the change.
- The conflict is resolved as `made_head`. Every other unresolved
  divergent copy at the same session and path whose bytes the new head
  extends is resolved as `superseded`, checked against the CAS.
- The next post from the machine that rewrote the file then extends
  the new head and moves it, instead of adding another copy.
- The head that was replaced stays stored. If a machine keeps posting
  the old lineage, those posts become conflicts, which is the truth.
- Refused when the session's head moved since the page was read, so an
  operator never replaces bytes they did not see.

## Acceptance criteria

- [ ] Make this the head moves the session head and current row, records a head_updates row and queues normalization
- [ ] Older unresolved copies at the same path that the new head extends are resolved as superseded
- [ ] A later post that extends the new head moves it instead of adding a copy
- [ ] A head that moved since the page was read refuses the change
