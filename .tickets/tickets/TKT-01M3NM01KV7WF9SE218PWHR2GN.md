---
schema: 3
id: TKT-01M3NM01KV7WF9SE218PWHR2GN
title: "Profile preview: list the projects a change admits and drops"
type: task
status: ready
status_reason: null
priority: high
due_on: null
labels:
  - area/server
assignees: []
milestone: null
parent: TKT-01M3NM01J4731BSWEA815FWGXS
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-29T03:40:36Z
updated_at: 2026-09-29T03:41:08Z
created_by:
  id: agent:claude-code/58fb7d84
  name: ""
updated_by:
  id: agent:claude-code/58fb7d84
  name: ""
extensions: {}
---

## Description

The profile editor's preview shows the line diff, the changed fields and the devices the profile reaches. It does not show what the change does to those devices' projects. A `git_remote_prefix`, or any wider rule, can admit projects nobody looked at, and the operator cannot see which ones before saving.

### Approach

For each active device that fetches the profile, read its newest inventory (`DeviceInventoryOf`) and evaluate every inventoried project under the stored profile's `projects` and under the edited one, with `config.Projects.Permitted` on a `config.ProjectID` built from the row (cwd, cwd hash, git remote). List the projects whose verdict changes, grouped by project key (`catalog.ProjectKeyOf`) across devices:

- **Admits:** refused under the stored rules, permitted under the edited rules.
- **Drops:** permitted under the stored rules, refused under the edited rules.

Each row names the key, the devices, and the session count. Put the list on the HTML preview, and on the JSON preview if the API has one.

### Limits the preview must state

- A device whose `config.json` sets its own allow rules (`allow_source` local) is not reached by profile allow rules. Leave it out and keep the existing `LocalAllow` count.
- A STRICT device sends no refused rows. Say how many devices on the profile are strict or have sent no inventory, since the list cannot speak for them.
- The lake cannot see a device's local deny rules. A row the device refused with `a deny rule matches` stays refused whatever the profile's allow rules say, so it is never listed as admitted.

### Files

- `internal/web/profile_edit.go` (`preview`, `profilePreview`)
- `internal/web/templates/page.html`
- `docs/web-dashboard.md`, and `docs/web-api.md` if the JSON shape changes

## Acceptance criteria

- [ ] The preview lists each project whose verdict changes, as admitted or dropped, with its devices and session count
- [ ] Devices with local allow rules are left out, and strict devices and devices with no inventory are counted
- [ ] A row the device refused under a deny rule is never listed as admitted
- [ ] Tests cover admit, drop, local allow, strict and deny rows; docs describe the list
