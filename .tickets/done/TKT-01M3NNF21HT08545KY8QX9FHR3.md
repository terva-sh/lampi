---
schema: 3
id: TKT-01M3NNF21HT08545KY8QX9FHR3
title: "Bays: catalog tables, membership audit, grants and migration"
type: task
status: done
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
updated_at: 2026-09-29T16:05:43Z
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

- [x] Migration puts every existing session in default and gives existing devices write on default
- [ ] serve backup, restore, fsck and purge cover the new tables
- [x] Bay name syntax is decided and enforced, with aliases unique across names
- [x] Every membership change queues an audit event to the existing outbox in the same transaction

## Implementation plan

One schema step, migrateBays (schema 18): bays, bay_aliases, session_bays (many-to-many, indexed by bay), session_bay_requests (latest request per session and bay ref, with outcome and reason, for the routing child), and bay_grants (principal kind group, device or read_token; read or write). The migration creates bay_default named default, puts every stored session in it and grants every device write on it. New sessions land in default inside the ingest transaction and new devices get write on default when made, with no audit line, since routing replaces that. Membership and grant changes queue audit events in the same transaction through the existing outbox. Purge removes a session's membership and request rows; backup is VACUUM INTO, so it covers the tables. Holds and review flags are left to the routing child's own migration so each PR stays small.

## Notes

**agent:claude-code/7859b064** at 2026-09-29T16:05:43Z

AC2 left unticked. serve backup (VACUUM INTO) and restore copy the whole catalog, so the bay tables go with them, and purge deletes session_bays and session_bay_requests (session_holds from TKT-01M3NNF29W). But serve fsck only re-hashes CAS objects and checks no catalog invariant, so nothing reports a session in no bay or a membership naming a deleted bay. That check is carried to TKT-01M3NNF2FE (Bays: inbox tooling), which is where membership health is reported.

## Summary

Merged in PR #150 (785f745). Schema 19, `migrateBays`, runs after main's 18 (`migrateConflictResolutions`), which landed while the PR was open. It adds:

- `bays`, `bay_aliases`, `session_bays`, `session_bay_requests` and `bay_grants`;
- triggers that keep a bay name and an alias apart, on insert and on rename, from reviews 1392 and 1394.

The migration seeds the default bay, puts every stored session in it and grants every device write on it.

Catalog API: create, resolve, membership add and remove, and grants, each audited through the outbox in the same transaction.

Behaviour:
- A new session lands in the default.
- With the default turned off, a new session that nothing places is refused. That applies at ingest only (review 1388).
- A stored session that loses its last bay still goes to the default when it is off (review 1390 rejected; docs/policy.md#bays).
