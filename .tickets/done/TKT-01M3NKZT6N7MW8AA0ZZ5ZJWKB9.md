---
schema: 3
id: TKT-01M3NKZT6N7MW8AA0ZZ5ZJWKB9
title: "Dashboard: admin role above operator"
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/auth
  - area/server
  - policy
assignees: []
milestone: null
parent: TKT-01M3NM61CZDGNG6K2XXGHECC0W
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-29T03:40:29Z
updated_at: 2026-09-29T04:06:53Z
created_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
extensions: {}
---

## Description

Add an `admin` role to the dashboard, above `operator`. Today
`role_map` grants only `viewer` and `operator`
(`internal/webconfig/config.go:90`), and `webauth.OperatorOnly` is the
only elevated gate.

This ticket exists so that the admin role lands once, and every effort
that needs it depends on it rather than each building its own:

- TKT-01M3NKY2V3 (Dashboard: admin-only raw artifact view) and
  TKT-01M3NM6FW7 (Raw-read tokens) gate raw bytes on it.
- TKT-01M3N8KHW5 (Bays: segment one lake and route sessions to a bay)
  decided in grilling round 4 that admin and operator are different
  roles: admin reads everything and owns bays, rules and grants, while
  operator mints registration codes and may be scoped to bays. As of
  2026-09-28 that epic is on the unpushed branch
  `t3code/share-ponds-within-sessions`.
- TKT-01M3KAMD1Z (Lake-backed sessions: read path, token scope,
  byte-exact reads) plans read tokens, which need someone to issue
  them. It is on the unpushed branch `t3code/session-lake-epic`.

On 2026-09-28 neither branch had an admin role in code.

### What admin means in this ticket

- `admin` implies `operator`, which implies `viewer`.
- Admin adds only the new powers: raw artifact reads and raw-read
  tokens. Operators keep every power they have today. This ticket moves
  nothing from operator to admin.
- Admin-only routes answer 404 to non-admins, the way operator routes
  do now.

### Upgrade: existing operators are not promoted

Settled 2026-09-28 by agent:claude-code/cd41c9ac, working autonomously
for the owner. Review it before Bays builds on it.

A config with no admin group starts, keeps working exactly as before,
and logs at startup that no group holds admin. Nobody gains raw read
access until an admin group is added to `role_map` by hand.

The alternative was promoting every operator group to admin on upgrade,
as the Bays epic proposes for its own migration. That lost here because
it would give raw, unredacted read access to every existing operator
without anyone deciding it. The Bays proposal keeps behavior unchanged
because in Bays operator *loses* powers to admin. Here operator loses
nothing, so no promotion is needed to keep behavior unchanged. When
Bays moves powers from operator to admin, its migration can promote
then.

### Out of scope

Bay grants, per-principal permissions, and changes to OIDC group sync.

## Acceptance criteria

- [x] role_map accepts admin; admin implies operator and viewer; unknown roles still fail startup
- [x] webauth has an AdminOnly gate that answers 404 to non-admins, with tests
- [x] Startup logs which groups hold admin, and warns when none does
- [x] docs/web-dashboard.md documents the three roles and what each can do
- [x] Operator groups are not promoted on upgrade; a config with no admin group behaves exactly as before

## Implementation plan

Add RoleAdmin to webconfig (accepted by Validate, AdminGroups for logging). Identity.Admin set by the provider, implying Operator and Viewer. webauth.AdminOnly shares the 404 gate with OperatorOnly. startWeb logs admin groups, or warns when none. Docs: web-dashboard.md step 4 and the example config. Nothing uses AdminOnly yet; TKT-01M3NKY2V3 is the first route behind it.

## Summary

Landed in #132 (merge dde83a3), reviewed clean at 6b2437c. role_map accepts admin, which implies operator and viewer; webauth.AdminOnly answers 404 to operators and viewers; serve logs the admin groups or warns when there are none. No group is promoted on upgrade (decision and rationale in the description). terva-review 1319 had one low finding, that the guide promised raw reads before any route used AdminOnly; the wording was fixed in 6b2437c and the disposition posted. The first AdminOnly routes arrive with TKT-01M3NKY2V3.
