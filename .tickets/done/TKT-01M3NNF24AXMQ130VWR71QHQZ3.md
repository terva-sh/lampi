---
schema: 3
id: TKT-01M3NNF24AXMQ130VWR71QHQZ3
title: "Bays: bay scope for operators, viewers and read tokens"
type: task
status: done
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
updated_at: 2026-09-29T16:49:29Z
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
- [x] An operator without a read grant cannot read session content
- [x] The regcode format is unchanged
- [x] Upgrade keeps behavior: viewers and operators read default, no group becomes admin, and startup logs the grants
- [x] A read token with a bay list reads only sessions in those bays; tokens minted before bays keep their scope

## Notes

**agent:claude-code/7859b064** at 2026-09-29T16:49:23Z

AC1 is half done. An operator cannot mint a code into a bay outside its write scope (checkMintBays; 403 bay_not_allowed, tested). The dashboard does not edit device bay grants yet: grants are changed only with serve bays grant on the lake host, which is admin access. So no operator can edit one out of scope, but the feature the AC describes is not built. It is carried to the triage follow-up TKT-01M3NNF2NN, and TKT-01M3NNF2K3 records the gap.

## Summary

Landed in two PRs.

**#152 (a88196c).**
- `serve bays` manages bays, aliases, delete, the default switch and grants.
- `Identity.Groups` carries IdP groups. Role groups are kept first under the 256 cap, and a role comes only from a kept group (reviews 1401 and 1402).
- `SeedRoleGrants` grants viewer and operator groups the default once per lake. No group is made admin, and every start flushes queued audit lines (review 1403).
- A rename keeps the old name as an alias, and a bay can take its own alias back (review 1405).

**Second PR.** Registration codes carry bays, granted on redeem:
- An operator mints only into its write bays.
- The regcode format is unchanged.
- Read tokens can be limited by bay. Tokens minted before bays keep their scope.

Dashboard editing of device grants is not built; see the note on AC1.
