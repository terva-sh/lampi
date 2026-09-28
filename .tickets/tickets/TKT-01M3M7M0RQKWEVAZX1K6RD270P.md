---
schema: 3
id: TKT-01M3M7M0RQKWEVAZX1K6RD270P
title: "Agent heartbeat: durable last contact, sync counters, applied profile"
type: task
status: in-progress
status_reason: null
priority: high
due_on: null
labels:
  - area/protocol
  - area/catalog
  - area/agent
assignees: []
milestone: null
parent: TKT-01M3M7KB32E2BCFEA9CN710522
origin: null
dependencies: []
blocks_on: none
references: []
claim:
  actor: agent:claude-code/2cf53976
  branch: t3code/add-agent-configuration
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-2cf53976
  commit: 740d778b278d87fc0ec7d2721efb170b44665f74
  session: null
  claimed_at: 2026-09-28T15:12:28Z
  expires_at: null
archive: null
created_at: 2026-09-28T14:45:05Z
updated_at: 2026-09-28T15:12:28Z
created_by:
  id: agent:claude-code/2cf53976
  name: ""
updated_by:
  id: agent:claude-code/2cf53976
  name: ""
extensions: {}
---

## Description

Agents tell the lake they are alive and what their last sync did. Today the lake keeps last contact only in memory (`internal/api/server.go` contacts map), lost when `serve` restarts, and the sync counters exist only in the agent's `last_sync.json`.

- New authenticated endpoint, e.g. `POST /v1/agent/report`, sent after each sync and at least every few minutes while idle.
- Body: agent version, inventory mode (SOCIABLE or STRICT), applied profile name and version, where each part of the effective config came from (`allow_source`, `deny_source`, as `agent config` prints them), last sync time and counters (checked, missing, uploaded, manifests, refused, quarantined, unchanged), and the last error.
- The lake stores the newest report per device in the catalog, including a durable `last_seen`.
- The Machines table on `/operations` reads from the catalog rather than from memory.

`allow_source` is important: a machine with local allow rules ignores the lake's allow list (`ApplyLakeProfile`), and the dashboard has to say so, or an edit that has no effect looks broken.

## Acceptance criteria

- [ ] Agents report after each sync and periodically when idle
- [ ] The lake stores last_seen and the newest report per device durably
- [ ] The operations Machines table reads last contact from the catalog
