---
schema: 3
id: TKT-01M3NKZT6N7MW8AA0ZZ5ZJWKB9
title: "Dashboard: admin role above operator"
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/auth
  - area/server
  - policy
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-29T03:40:29Z
updated_at: 2026-09-29T03:40:29Z
created_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
extensions: {}
---

## Description

Add an `admin` role to the dashboard, separate from and above
`operator`. Today `role_map` grants only `viewer` and `operator`
(`internal/webconfig/config.go:90`), and `webauth.OperatorOnly` is the
only elevated gate.

This ticket exists so that the admin role lands once, and every effort
that needs it depends on it rather than each building its own:

- TKT-01M3NKY2V3 (Dashboard: admin-only raw artifact view) needs admin
  to gate raw bytes and to mint raw-read tokens.
- TKT-01M3N8KHW5 (Bays: segment one lake and route sessions to a bay)
  decided in grilling round 4 that admin and operator are different
  roles: admin reads everything and owns bays, rules and grants, while
  operator mints registration codes and may be scoped to bays. As of
  2026-09-28 that epic lives on the unpushed branch
  `t3code/share-ponds-within-sessions` and is not on main.
- TKT-01M3KAMD1Z (Lake-backed sessions: read path, token scope,
  byte-exact reads) plans read tokens, which need an admin to issue
  them. It lives on the unpushed branch `t3code/session-lake-epic`.

Before starting, check whether either branch has landed or started an
admin role, and build on it rather than beside it.

### Decisions carried from the Bays epic

- `admin` implies `operator`, which implies `viewer`. This ticket makes
  no bay decisions. Bays can narrow operator and viewer later without
  changing what admin means.
- On upgrade, behavior does not change. `role_map` accepts `admin`. A
  config with no admin group still starts, and startup logs that no
  group holds admin, because denying a lake with no admin would lock
  out every existing deployment. Whether existing operator groups are
  promoted to admin automatically (as Bays proposes) or only by an
  explicit config edit is the one open question. Settle it with the
  owner before implementing.
- Admin-only routes return 404 to non-admins, the way operator routes
  do today.

### Out of scope

Bay grants, per-principal permissions, and OIDC group sync changes.

## Acceptance criteria

- [ ] role_map accepts admin; admin implies operator and viewer; unknown roles still fail startup
- [ ] webauth has an AdminOnly gate that answers 404 to non-admins, with tests
- [ ] Upgrade behavior for existing operator groups is decided with the owner and recorded in this ticket
- [ ] Startup logs which groups hold admin, and warns when none does
- [ ] docs/web-dashboard.md documents the three roles and what each can do
