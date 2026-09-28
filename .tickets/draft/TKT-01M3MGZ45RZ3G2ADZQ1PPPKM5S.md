---
schema: 3
id: TKT-01M3MGZ45RZ3G2ADZQ1PPPKM5S
title: Agent auto-update when the lake runs a newer release (opt-in)
type: task
status: draft
status_reason: null
priority: low
due_on: null
labels:
  - area/agent
  - area/ops
assignees: []
milestone: null
parent: TKT-01M3MAV1XM089JPDCG5NH2RAJ6
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-28T17:28:26Z
updated_at: 2026-09-28T17:28:26Z
created_by:
  id: agent:claude-code/2cf53976
  name: ""
updated_by:
  id: agent:claude-code/2cf53976
  name: ""
extensions: {}
---

## Description

Follow-up to the self-update ticket. The owner deferred this on 2026-09-28.

An agent whose local `config.json` opts in, for example with `"auto_update": true`, upgrades itself when its lake advertises a newer release. The opt-in would work like STRICT mode: the lake cannot set it.

This needs rollback on a failed health check, a quiet window so an upgrade does not interrupt a sync, and a report of the outcome in the heartbeat. The fleet epic currently records "this epic only warns".
