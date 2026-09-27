---
schema: 3
id: TKT-01M3J5HXAJBA1VR4W4B5H4NB6G
title: "Dashboard: list, revoke and set profiles for devices"
type: task
status: draft
status_reason: null
priority: low
due_on: null
labels:
  - area/server
  - area/auth
assignees: []
milestone: null
parent: null
origin: null
dependencies:
  - TKT-01M3J5HX9FY5M7V2VF162Z9FX3
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-27T19:30:30Z
updated_at: 2026-09-27T19:30:30Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/e4a47e8c
  name: ""
extensions: {}
---

## Description

Follow-up to the dashboard registration-code page. Use the same `operator` role to list devices (name, machine id, profile, created, last upload, state), revoke a device, unbind a token device's machine, and set a device's profile. These are the `serve devices` subcommands.

Each action is a CSRF-checked POST. Revoking asks for confirmation that names the device, and the audit log carries the OIDC actor. The registration list then links a used code to its device.

## Acceptance criteria

- [ ] Operators list devices and revoke, unbind or set a profile
- [ ] Actions are CSRF-checked and audited with the OIDC actor
