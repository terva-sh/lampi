---
schema: 3
id: TKT-01M3NNF24AXMQ130VWR71QHQZ3
title: "Bays: bay scope for operators, viewers and read tokens"
type: task
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area/auth
  - area/server
assignees: []
milestone: null
parent: TKT-01M3N8KHW56GVZXHV0KBNSPEX1
origin: null
dependencies:
  - TKT-01M3NNF21HT08545KY8QX9FHR3
blocks_on: none
references: []
claim:
  actor: agent:claude-code/7859b064
  branch: bays/scope-cli
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-7859b064
  commit: dd8a4989444116fa65943c2381ba12f8bc831274
  session: null
  claimed_at: 2026-09-29T16:05:43Z
  expires_at: null
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

Scope operators, viewers and read tokens by bay, and give devices their write bays at registration. Design in the parent epic TKT-01M3N8KHW5 (Bays: segment one lake and route sessions to a bay).

The admin role already exists (TKT-01M3NM61CZ, Admin role and raw artifact access): admin implies operator implies viewer, admins read raw artifacts and mint `lrt_` read tokens, and no group is promoted to admin on upgrade. This ticket adds bay scope under it and does not change the role hierarchy.

### Scope

- Bay read grants for OIDC groups. A viewer or operator reads only the bays its groups are granted. An admin reads every bay.
- Operators are limited to the bays they hold write scope on for minting and device edits.
- On upgrade: existing viewer and operator groups get read on `default` and existing operators get write scope on every bay, so nothing changes. No group becomes admin (owner decision on TKT-01M3NM61CZ, 2026-09-29). Startup logs which groups were granted what.
- `serve register --bay NAME` (repeatable), and the dashboard's mint form, put write grants on the pending registration row beside the profile, capped at the minting operator's scope. Redeeming the code copies them onto the device. The code format in `internal/regcode` does not change.
- `serve devices` and the dashboard gain bay grant edits, allowed only to an operator whose scope covers the bays involved, or an admin. A dashboard edit that adds access requires a sign-in in the last 10 minutes.
- Read tokens gain an optional bay list, evaluated on each read. Tokens minted before bays keep their scope. Coordinate with the broader token model in TKT-01M3KAMD1Z.
- `serve bays` create, rename (old name kept as an alias), alias, delete (sessions left in no bay move to `default`, no data deleted) and default on/off, admin only. On the host CLI, admin means host access to the lake directory.
- Every grant and bay change goes to the audit outbox.

## Acceptance criteria

- [ ] An operator cannot mint a code or edit a device grant for a bay outside its scope
- [ ] An operator without a read grant cannot read session content
- [ ] The regcode format is unchanged
- [ ] Upgrade keeps behavior: viewers and operators read default, no group becomes admin, and startup logs the grants
- [ ] A read token with a bay list reads only sessions in those bays; tokens minted before bays keep their scope
