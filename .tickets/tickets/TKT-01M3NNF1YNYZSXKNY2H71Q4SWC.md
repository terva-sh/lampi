---
schema: 3
id: TKT-01M3NNF1YNYZSXKNY2H71Q4SWC
title: "Bays: amend policy, architecture and naming docs"
type: task
status: blocked
status_reason: "Docs merged in #148 (9936b1c). Waiting only for the owner to read the Bays section of docs/policy.md and sign off (AC 2)."
priority: normal
due_on: null
labels:
  - area/docs
  - policy
assignees: []
milestone: null
parent: TKT-01M3N8KHW56GVZXHV0KBNSPEX1
origin: null
dependencies: []
blocks_on: none
references: []
claim:
  actor: agent:claude-code/7859b064
  branch: bays/policy-docs
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-7859b064
  commit: 0afc10f1e1a1973ecb897701a561647a725baf67
  session: null
  claimed_at: 2026-09-29T14:58:27Z
  expires_at: null
archive: null
created_at: 2026-09-29T04:06:17Z
updated_at: 2026-09-29T15:21:34Z
created_by:
  id: agent:claude-code/7859b064
  name: ""
updated_by:
  id: agent:claude-code/7859b064
  name: ""
extensions: {}
---

## Description

Amend the recorded decisions before any bay code lands, so the docs and the code do not disagree. The design is in the parent epic TKT-01M3N8KHW5 (Bays: segment one lake and route sessions to a bay); read its decisions before writing.

### Scope

- `docs/policy.md`: record bays as an access boundary inside one lake. This reopens, for access within one lake, "multi-tenant lakes", which TKT-01M3FHHBC listed as out of scope. Record the admin/operator split and that the default bay is readable only by admins and explicit grants.
- `docs/architecture.md`: membership lives only in the catalog, the blob store and derived views stay shared, and every read path joins against membership. Record the known `blobs/check` limit and that filesystem access to the lake directory is admin-level.
- `docs/naming.md`: add why the word is "bay", and the rejected names (pond, partition, space, collection).

The owner signs off on the policy change in a ticket note or the PR.

## Acceptance criteria

- [x] docs/policy.md, docs/architecture.md and docs/naming.md record bays as decided in the epic
- [ ] The owner's sign-off on the policy change is recorded

## Notes

**agent:claude-code/7859b064** at 2026-09-29T15:00:00Z

Docs written at aa88bad: docs/policy.md gains a Bays section (model, known limits) and a retention line; docs/architecture.md gains the storage consequence under Auth; docs/naming.md gains why bay. Sign-off: the owner decided every point in the grilling on 2026-09-29 (notes on TKT-01M3N8KHW5) and asked for the epic to be worked. The wording itself has not been read by the owner; it is flagged for review in the PR.

**agent:claude-code/7859b064** at 2026-09-29T15:04:45Z

Review 1377 on #148 (head 14bca02): finding-1 accepted, the retention line now says only a session left in no bay moves to the default, matching the epic. finding-2 accepted: a refused request no longer lands in the default unconditionally; it places nothing, and a session left with no bay lands in the default or, with the default off, is refused like any unplaced session. This is the agent's reading of the owner's round 3 and round 3 Q11 answers together, for the owner to confirm; TKT-01M3NNF29W carries it. finding-3 accepted: AC 2 (owner sign-off) is unticked. The decisions are the owner's from the grilling, but the owner has not read this wording, so the ticket stays open for sign-off after merge.

**agent:claude-code/7859b064** at 2026-09-29T15:21:34Z

in-progress to blocked: Docs merged in #148 (9936b1c). Waiting only for the owner to read the Bays section of docs/policy.md and sign off (AC 2).
