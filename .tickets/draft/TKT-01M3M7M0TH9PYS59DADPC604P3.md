---
schema: 3
id: TKT-01M3M7M0TH9PYS59DADPC604P3
title: Agent inventory report of seen projects, gated by inventory mode
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/agent
  - area/protocol
  - area/catalog
assignees: []
milestone: null
parent: TKT-01M3M7KB32E2BCFEA9CN710522
origin: null
dependencies:
  - TKT-01M3M7M0PMGW8JG0Y45RFPY34M
  - TKT-01M3M7M0RQKWEVAZX1K6RD270P
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-28T14:45:05Z
updated_at: 2026-09-28T14:45:05Z
created_by:
  id: agent:claude-code/2cf53976
  name: ""
updated_by:
  id: agent:claude-code/2cf53976
  name: ""
extensions: {}
---

## Description

In SOCIABLE mode the agent reports what it can see, so the dashboard can show each device's sessions and offer to allow a refused project.

- One row per project the harness roots contain: harness, cwd, cwd_hash, normalized git remote, session count, total bytes, newest session time, allowed or refused, and the `Refusal` reason.
- Built from the same scan and the same `Projects.Permitted` check that `agent refused` uses, so the two cannot disagree.
- STRICT mode sends allowlisted rows only (see the policy ticket for the aggregate count).
- Sent as a full snapshot when it changes (compare with a hash of the last one sent), not on every sync.
- The lake stores the newest snapshot per device, replacing the previous one. Paths are metadata, but they are sensitive, so the snapshot is not kept as history.
- Git reads for projects outside the allowlist follow the same confinement as today (see TKT-01M3B60K); the inventory must not open more of a repository than the allowlist check already does.

## Acceptance criteria

- [ ] SOCIABLE agents report every project with counts, sizes, paths and verdict
- [ ] STRICT agents report allowlisted projects only; profiles cannot change the mode
- [ ] The report agrees with agent refused on the same machine
