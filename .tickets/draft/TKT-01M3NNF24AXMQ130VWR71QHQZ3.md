---
schema: 3
id: TKT-01M3NNF24AXMQ130VWR71QHQZ3
title: "Bays: admin role, bay-scoped operators, grants on registration"
type: task
status: draft
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

Split admin from operator and scope operators by bay. Design in the parent epic TKT-01M3N8KHW5 (Bays: segment one lake and route sessions to a bay).

### Scope

- Web config gains an `admin` role mapping. Admin reads every bay and manages bays, rules and grants. Operator mints codes and manages devices, is limited to the bays it is granted write scope on, and reads session content only in bays it holds a read grant on. Viewer reads only its granted bays.
- On upgrade: existing viewer groups get read on `default`, existing operator groups become operators scoped to all bays and also admins. Startup logs which groups were granted what.
- `serve register --bay NAME` (repeatable) puts write grants on the pending registration row, beside the profile, capped at the minting operator's scope. Redeeming the code copies them onto the device. The code format in `internal/regcode` does not change.
- `serve devices` gains bay grant edits, allowed only to an operator whose scope covers the bays involved, or an admin.
- `serve bays` create, rename (old name kept as an alias), alias, delete (sessions left in no bay move to `default`, no data deleted) and default on/off, admin only. On the host CLI, admin means host access to the lake directory.

## Acceptance criteria

- [ ] An operator cannot mint a code or edit a device grant for a bay outside its scope
- [ ] An operator without a read grant cannot read session content
- [ ] Upgrade keeps behavior: viewers read default, operators become admins, and startup logs the grants
- [ ] The regcode format is unchanged
