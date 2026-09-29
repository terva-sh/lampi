---
schema: 3
id: TKT-01M3NNF21HT08545KY8QX9FHR3
title: "Bays: catalog tables, membership audit, grants and migration"
type: task
status: draft
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
claim: null
archive: null
created_at: 2026-09-29T04:06:17Z
updated_at: 2026-09-29T04:06:17Z
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

- Tables: bays (stable id, name, aliases, whether it is the default, whether the default is turned off), session-to-bay membership (many-to-many), the bays each manifest requested with any refusal and its reason, a membership audit log (who, when, why, from which path: rule, CLI, web), and grants (principal, bay, read or write).
- Schema version bump with a migration that creates `default`, puts every existing session in it, and makes the upgrade grants: existing devices write on `default`. Role-based grants for OIDC groups come with the roles child.
- Catalog functions for adding, removing and listing membership, each writing an audit row in the same transaction.
- `serve backup`, restore, `fsck` and `purge` cover the new tables. `purge` of a session removes its membership and requested-bay rows.
- Decide bay name syntax here (for example lowercase letters, digits, dash) and reject a name that collides with an alias.

No bay other than `default` can hold data yet: the read-path child lands before routing.

## Acceptance criteria

- [ ] Migration puts every existing session in default and gives existing devices write on default
- [ ] Every membership change writes an audit row in the same transaction
- [ ] serve backup, restore, fsck and purge cover the new tables
- [ ] Bay name syntax is decided and enforced, with aliases unique across names
