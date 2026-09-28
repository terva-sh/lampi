---
schema: 3
id: TKT-01M3MQM6H3V8QK2RTRNJBQKXQK
title: Device actions look a device up by id but change it by name
type: bug
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-28T19:24:48Z
updated_at: 2026-09-28T21:32:43Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/2cf53976
  name: ""
extensions: {}
---

## Description

terva-review raised this on PR #94 (run 58803ce2), whose merge from
main carried #88's device actions; it is not in #94's change.

`internal/web/device_actions.go` finds the device by its `dev_` id
(`deviceByID`), then calls `RevokeDevice`, `UnbindDevice` and
`SetDeviceProfile` with the device's name. If the device is renamed or
removed between the lookup and the change, and its former name now
belongs to another device, the action changes that other device. The
`ErrNoDevice` check only catches a name that disappeared.

Make the mutators take the id, or check the id and change the row in
one transaction.

## Summary

Catalog gains DeviceByID and RevokeDeviceByID, UnbindDeviceByID and SetDeviceProfileByID. The name and id forms share one body with a lookup function; each UPDATE was already by the row's id, so only the lookup changes. The dashboard's device actions look up and change by the dev_ id, so a device whose name passed to another after the operator saw the list can no longer be changed in its place. serve devices keeps the name forms. TestDeviceChangesByIDFollowTheID renames rows under the lookup to prove it.
