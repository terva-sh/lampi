---
schema: 3
id: TKT-01M3PTMWM9243H1PQT6J3JM8BY
title: "Conflicts: operator makes a divergent copy the session head"
type: task
status: in-progress
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
claim:
  actor: agent:claude-code/cd41c9ac
  branch: feat/conflict-make-head
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-fdd1a9d1
  commit: 2618b715ce966303232331e8f173fa1eb3a3e108
  session: null
  claimed_at: 2026-09-29T15:24:04Z
  expires_at: null
archive: null
created_at: 2026-09-29T14:56:05Z
updated_at: 2026-09-29T15:24:05Z
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

- [x] Make this the head moves the session head and current row, records a head_updates row and queues normalization
- [x] Older unresolved copies at the same path that the new head extends are resolved as superseded
- [x] A later post that extends the new head moves it instead of adding a copy
- [x] A head that moved since the page was read refuses the change

## Notes

**agent:claude-code/cd41c9ac** at 2026-09-29T15:24:05Z

catalog.MakeConflictHead(blobs, id, expectHead, by, note, now), one transaction: the copy becomes current at its path and the session head, the old head row stops being current (same or other path), resolution made_head (relation stays divergent_copy), other open copies at the path that the new head extends (Relate through the CAS) resolved superseded, head_updates row relation=made_head attributed to MIN(machine) that posted the copy, normalize_gen bumped with a job row, audit conflict.resolved(s) + conflict.head_changed(old_head). Refusals: ErrHeadMoved (expectHead is the head the page showed), ErrNotHeadCandidate (companion of the head, non-head-bearing kind, other kind), ErrConflictResolved. Reopen of a copy that is its session's head now: ErrConflictIsHead, and the page hides Reopen there. Web: make-head form inside a details, hidden head field; Registrations.Normalize = lake.ReloadNormalizeJobs so the job runs now, not at next start. Mutation-checked: head check, clearing current, supersede filter, companion refusal, is-head refusal, normalize kick. No undo button: the replaced head stays stored and a later post of its lineage would show as a conflict; recorded as a decision rather than built.
