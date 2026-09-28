---
schema: 3
id: TKT-01M3MAV1XM089JPDCG5NH2RAJ6
title: "Device fleet visibility: agent versions, upgrades and management"
type: epic
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
  - area/agent
assignees: []
milestone: v0.2.0
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-28T15:41:21Z
updated_at: 2026-09-28T15:50:08Z
created_by:
  id: agent:claude-code/03b82158
  name: ""
updated_by:
  id: agent:claude-code/03b82158
  name: ""
extensions: {}
---

## Description

Improvements that let an operator see and manage the machines that sync to a lake. The operations page (TKT-01M3JV45Z, done) lists devices and when they last uploaded. This epic adds what that list leaves out: which agent build each device runs, whether it needs upgrading, and the operator actions `serve devices` offers on the command line.

Target: the next release.

### Children

- TKT-01M3MAPZZY Devices: show agent version, flag outdated and known-bad agents. The agent reports its version, the server compares it with its own build and against an embedded advisory list, and the dashboard shows a behind or urgent badge.
- TKT-01M3J5HXA Dashboard: list, revoke and set profiles for devices. Operator actions on devices, CSRF-checked and audited. Once the version ticket lands, this page shows its version column and badges too.

The two can ship in either order. Neither waits on the other.

### Out of scope

Refusing a known-incompatible agent at the server. That belongs to protocol versioning, and this epic only warns.

## Acceptance criteria

- [ ] Every device in the dashboard shows its agent version and upgrade state
- [ ] Operators manage devices from the dashboard without the CLI
