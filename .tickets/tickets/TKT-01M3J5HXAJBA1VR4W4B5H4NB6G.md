---
schema: 3
id: TKT-01M3J5HXAJBA1VR4W4B5H4NB6G
title: "Dashboard: list, revoke and set profiles for devices"
type: task
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
  - area/auth
assignees: []
milestone: v0.2.0
parent: TKT-01M3MAV1XM089JPDCG5NH2RAJ6
origin: null
dependencies:
  - TKT-01M3J5HX9FY5M7V2VF162Z9FX3
blocks_on: none
references: []
claim:
  actor: agent:claude-code/2cf53976
  branch: web/devices-list
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-2cf53976
  commit: 5b41022770000297c468d9f23d4a36475aff6c9d
  session: null
  claimed_at: 2026-09-28T18:12:20Z
  expires_at: null
archive: null
created_at: 2026-09-27T19:30:30Z
updated_at: 2026-09-28T18:12:20Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/2cf53976
  name: ""
extensions: {}
---

## Description

Follow-up to the dashboard registration-code page. Use the same `operator` role to list devices (name, machine id, profile, created, last upload, state), revoke a device, unbind a token device's machine, and set a device's profile. These are the `serve devices` subcommands.

Each action is a CSRF-checked POST. Revoking asks for confirmation that names the device, and the audit log carries the OIDC actor. The registration list then links a used code to its device.

## Acceptance criteria

- [ ] Operators list devices and revoke, unbind or set a profile
- [ ] Actions are CSRF-checked and audited with the OIDC actor

## Implementation plan

Three PRs. D1 (this branch, web/devices-list): read-only /devices page and GET /api/web/v1/devices, joining catalog devices, device_reports, machine activity, live contacts and the resolved profile. D2: operator-only CSRF-checked POSTs on /devices for revoke, unbind and set-profile, audited with the OIDC actor, reusing the catalog calls behind serve devices; used registration codes link to their device. The list lands first so the actions have a page to live on and viewers get the read side without the operator role.

## Notes

**agent:claude-code/2cf53976** at 2026-09-28T18:12:20Z

D1 on web/devices-list: /devices lists every device with agent version, profile state (current/stale/unknown against the version the lake would serve now), allow-rule source (warns when config.json allow rules override the profile), last sync counts incl. refused, last error and freshness. Also JSON at /api/web/v1/devices. Actions are D2.
