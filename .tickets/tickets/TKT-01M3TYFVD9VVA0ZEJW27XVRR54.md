---
schema: 3
id: TKT-01M3TYFVD9VVA0ZEJW27XVRR54
title: Deploy v0.5.1 to the internal lake and workstation agent
type: task
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area/ops
assignees: []
milestone: null
parent: null
origin: null
dependencies:
  - TKT-01M3TQNYR8TK6K67S00MZ6ZECT
blocks_on: none
references: []
claim:
  actor: agent:claude-code/27b21f4b
  branch: release/v0.5.1
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-27b21f4b
  commit: a54eecc70229eef8b44d7b800fbbeebb1660a8ca
  session: null
  claimed_at: 2026-10-01T05:20:12Z
  expires_at: null
archive: null
created_at: 2026-10-01T05:20:12Z
updated_at: 2026-10-01T05:24:44Z
created_by:
  id: agent:claude-code/27b21f4b
  name: ""
updated_by:
  id: agent:claude-code/27b21f4b
  name: ""
extensions: {}
---

## Description

The owner asked on 2026-10-01 to deploy v0.5.1 once it was cut (TKT-01M3TQNYR8, Release v0.5.1). That request is this deploy's authorization, as TKT-01M3FP11A (Onboarding rollout: upgrade the hosted lake and register machines) requires for each deploy.

### Starting point

- The lake and the workstation agent run v0.5.0 (85480f5) at catalog schema 21 (TKT-01M3SYMZCW).
- v0.5.1 adds no migration, and the unit and web drop-in are byte-identical to the v0.5.0 bundle's copies.
- The diagnosis on 2026-10-01 found:
  - **role_map** maps a placeholder group `GROUP` to admin. The owner chose to make `Brokkr Lampi Admin` admin and remove `GROUP`.
  - **Two failed sessions**, 01M3NK7HRF (line 139) and 01M3NNF6B0 (line 691), failed on torn lines, which v0.5.1 turns into markers (TKT-01M3NQ2R).

### Approach

The v0.5.0 bundle's script, with the versions and expected schema changed, the bays block removed, and three additions:
- **Precondition:** role_map must equal the diagnosed map exactly.
- **role_map rewrite:** at install, keeping web.json's other keys, owner and mode, followed by a check that serve logs `Brokkr Lampi Admin` as the admin group.
- **Retry:** `serve normalize --failed` and a SIGHUP, with a wait until no session is failed or queued.

The workstation agent is upgraded to v0.5.1 afterwards for consistency, although v0.5.1 changes nothing it runs.

## Acceptance criteria

- [ ] A verified checkpoint of the stopped schema-21 lake exists, taken with v0.5.0, holding the old web.json
- [ ] The lake runs v0.5.1 at schema 21 with integrity ok, counts preserved, and health, 401s and the public URL answering
- [ ] role_map maps Brokkr Lampi Admin to admin and Brokkr Lampi User to viewer, with no GROUP, and serve logs Brokkr Lampi Admin as the admin group
- [ ] No session is left failed: 01M3NK7HRF and 01M3NNF6B0 normalize
- [ ] The workstation agent runs v0.5.1, stays pinned on lake:default, and its next sync re-uploads nothing

## Implementation plan

1. The owner runs deploy-v0.5.1-SMzZT776/operator-deploy.sh (in the external handoff) as root. It is the v0.5.0 script with the versions changed, the bays block removed, a precondition that role_map equals the diagnosed map, the role_map rewrite at install with a check of serve's admin-groups line, and normalize --failed plus SIGHUP with a two-minute wait.
2. The agent upgrades the workstation agent to v0.5.1, keeps copies of the old binary and config.json, and checks the pin and that the next sync uploads nothing.
3. The owner signs in and confirms the admin links appear for Brokkr Lampi Admin.

Tested on 2026-10-01: the upgrade and the retry against a v0.5.0 scratch lake with a torn line; the role_map precondition and rewrite against a copy of the diagnosed web.json (other keys and mode 0640 kept, a second run refused); and serve starting with the rewritten config and logging 'web admin groups groups="Brokkr Lampi Admin"'. The bundle's README carries the rollback: a binary swap, plus the checkpoint's web.json.
