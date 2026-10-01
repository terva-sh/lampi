---
schema: 3
id: TKT-01M3PTMWM9243H1PQT6J3JM8BY
title: "Conflicts: operator makes a divergent copy the session head"
type: task
status: done
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
updated_at: 2026-09-29T18:04:49Z
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

**agent:claude-code/cd41c9ac** at 2026-09-29T17:29:40Z

terva-review on #155: 1420 (make-head offered without a blob store), 1421 (failed normalize kick reported as success; head update attributed by digest across paths), 1426 (open copy with the head's bytes at another path lost its actions; kick skipped after a failed audit flush). All accepted and fixed with mutation-checked tests; dispositions posted on #155.

## Summary

Merged as #155 (main 17e0edb). catalog.MakeConflictHead makes an open divergent copy the session head in one transaction: current at its path, the old head row no longer current, resolved made_head, other open copies at its path that it extends resolved superseded, a head_updates row attributed to a machine that posted it at its path, normalize generation bumped with a job row, and conflict.resolved plus conflict.head_changed audited. It refuses a head that moved since the page was read, a companion file, another kind, and a resolved conflict; a copy that is its session's head cannot be reopened. The web action, offered only where the blob store is, kicks normalization after the audit flush and reports audit_failed or normalize_failed with the change standing. It is scoped by the caller's bays (after #156). Five terva-review rounds; every finding was accepted and fixed, with dispositions posted on #155.
