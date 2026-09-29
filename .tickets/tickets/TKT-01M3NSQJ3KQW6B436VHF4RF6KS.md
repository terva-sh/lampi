---
schema: 3
id: TKT-01M3NSQJ3KQW6B436VHF4RF6KS
title: Deploy v0.4.0 to the internal lake and workstation agent
type: task
status: in-progress
status_reason: null
priority: high
due_on: null
labels:
  - area/ops
assignees: []
milestone: null
parent: null
origin: null
dependencies:
  - TKT-01M3NSQJ1T5ZKJ2P8TQH6SJZJZ
blocks_on: none
references: []
claim:
  actor: agent:claude-code/16ebd168
  branch: release/v0.4.0
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-16ebd168
  commit: 1740c088c9eda3de9afcf565aeb14fc586dd297f
  session: null
  claimed_at: 2026-09-29T05:20:58Z
  expires_at: null
archive: null
created_at: 2026-09-29T05:20:50Z
updated_at: 2026-09-29T05:25:42Z
created_by:
  id: agent:claude-code/16ebd168
  name: ""
updated_by:
  id: agent:claude-code/16ebd168
  name: ""
extensions: {}
---

## Description

The owner asked on 2026-09-29 to cut a release carrying `lakes adopt` and run it. That request is this deploy's authorization, as TKT-01M3FP11A (Onboarding rollout: upgrade the hosted lake and register machines) requires for each deploy.

### Starting point

- The lake and the workstation agent run v0.3.0 (a43c5ce), catalog schema 16 (TKT-01M3NJ805R). `serve compact` has been run.
- The workstation agent is already adopted: TKT-01M3NMHDWR pinned it with a branch build, and it takes the default profile. It needs only the binary upgrade.
- v0.4.0 adds migration 17. The unit and web drop-in are byte-identical to the v0.3.0 bundle's.

### Approach

The v0.3.0 bundle's script, with the normalize and search steps removed and the versions and schema changed. The owner runs it as root:

1. Check every precondition before stopping anything.
2. Stop the lake and take a verified checkpoint with the installed v0.3.0's `serve backup` and `serve fsck`.
3. Install v0.4.0 and let serve migrate 16 → 17.
4. Check health, 401s, schema, integrity, counts, lake id, the public URL and the admin-groups log line, then resume the agent.

The workstation agent is then upgraded in place. Adopting the other machines is each machine's own step, run there with the fingerprint from the lake host.

## Acceptance criteria

- [ ] A verified checkpoint of the stopped schema-16 lake exists, taken with v0.3.0
- [ ] The lake runs v0.4.0 at schema 17 with integrity ok and counts preserved
- [ ] Health, auth refusals and the public URL answer after the upgrade
- [ ] The workstation agent runs v0.4.0, stays pinned on the default profile, and its next sync re-uploads nothing

## Implementation plan

1. The owner runs `deploy-v0.4.0-0v5zrnQd/operator-deploy.sh` (in the external handoff) as root. It checks every precondition first, then stops the lake and takes a checkpoint with v0.3.0's `serve backup`, re-hashed by fsck. It installs v0.4.0, and serve migrates 16 → 17. The script checks health, the 401s (including the raw-read route), schema, integrity, counts, the lake id and the public URL, and prints serve's admin-groups line. It then resumes the agent and waits for a sync.
2. The agent upgrades the workstation agent binary, keeps a copy of the old one and config.json, and checks that the agent stays pinned on `lake:default` and that the next sync uploads nothing.

The script is the v0.3.0 bundle's, which ran on this lake on 2026-09-29, with the versions and schema changed, the normalize and search steps removed, and the raw-route 401 and admin-line checks added. The unit and web drop-in are byte-identical to the v0.3.0 bundle's copies. The bundle README carries the `lakes adopt` commands for the other machines, and a rollback that restores only the catalog.
