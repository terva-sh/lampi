---
schema: 3
id: TKT-01M3N22HGYDZCVMK66PNTGF9QM
title: "Dashboard: view and edit a device's override with its note"
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
assignees: []
milestone: null
parent: TKT-01M3N22HCYT06PPNZFQP3YCCCE
origin: null
dependencies:
  - TKT-01M3N22HEQ6PJ1A9F56ZQN9M8B
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-28T22:27:24Z
updated_at: 2026-09-28T22:27:24Z
created_by:
  id: agent:claude-code/2cf53976
  name: ""
updated_by:
  id: agent:claude-code/2cf53976
  name: ""
extensions: {}
---

## Description

Show and edit overrides in the dashboard.

- **Device page.** A section shows the device's override, its note, who set it and when, and the effective profile it produces. Operators edit it with the profile editor's rule rows and preview. The note is required, and the save names the revision it read.
- **Profile page.** The reserved "Device overrides" tab lists the devices on this profile that carry an override, with each note and a link to the device.
- **Devices list.** It marks a device that has an override.
- **API.** `GET`/`PUT`/`DELETE /api/web/v1/devices/{id}/override`, operator-only for writes, CSRF-checked, with `409` on a stale revision and `400` on a missing note, following the profile API.

## Acceptance criteria

- [ ] The device page shows the override, its note and the effective profile
- [ ] Operators edit and clear an override with a required note, guarded by revision
- [ ] The profile page's Device overrides tab lists the devices with overrides and their notes
