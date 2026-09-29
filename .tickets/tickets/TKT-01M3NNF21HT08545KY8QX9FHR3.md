---
schema: 3
id: TKT-01M3NNF21HT08545KY8QX9FHR3
title: "Bays: catalog tables, membership audit, grants and migration"
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/catalog
assignees: []
milestone: null
parent: TKT-01M3N8KHW56GVZXHV0KBNSPEX1
origin: null
dependencies:
  - TKT-01M3NNF1YNYZSXKNY2H71Q4SWC
blocks_on: none
references: []
claim:
  actor: agent:claude-code/7859b064
  branch: bays/catalog
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-7859b064
  commit: 14bca02b5ce7bcd5390a9574f775b5e73264a12f
  session: null
  claimed_at: 2026-09-29T15:00:30Z
  expires_at: null
archive: null
created_at: 2026-09-29T04:06:17Z
updated_at: 2026-09-29T15:00:30Z
created_by:
  id: agent:claude-code/7859b064
  name: ""
updated_by:
  id: agent:claude-code/7859b064
  name: ""
extensions: {}
---

## Description

Catalog schema for bays. Design in the parent epic TKT-01M3N8KHW5 (Bays: segment one lake and route sessions to a bay).

### Scope

- Tables: bays (stable id, name, aliases, whether it is the default, whether the default is turned off), session-to-bay membership (many-to-many), the bays each manifest requested with any refusal and its reason, and grants (principal, bay, read or write), where a principal is an OIDC group, a device or a read token.
- Schema version bump with a migration that creates `default`, puts every existing session in it, and gives existing devices write on `default`. Grants for OIDC groups and read tokens come with the scope child.
- Catalog functions for adding, removing and listing membership. Each change queues an audit event (who, when, why, and whether a rule, the CLI or the web made it) to the existing audit outbox in the same transaction, as `internal/catalog` already does for devices and registrations. No separate audit table.
- `serve backup`, restore, `fsck` and `purge` cover the new tables. `purge` of a session removes its membership and requested-bay rows.
- Decide bay name syntax here (for example lowercase letters, digits, dash) and reject a name that collides with an alias.

No bay other than `default` can hold data yet: the read-path child lands before routing.

## Acceptance criteria

- [ ] Migration puts every existing session in default and gives existing devices write on default
- [ ] serve backup, restore, fsck and purge cover the new tables
- [ ] Bay name syntax is decided and enforced, with aliases unique across names
- [ ] Every membership change queues an audit event to the existing outbox in the same transaction
