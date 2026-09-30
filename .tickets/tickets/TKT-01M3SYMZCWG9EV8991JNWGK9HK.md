---
schema: 3
id: TKT-01M3SYMZCWG9EV8991JNWGK9HK
title: Deploy v0.5.0 to the internal lake and workstation agent
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
  - TKT-01M3RMFD64RRX4XW3J1RQ7C6ZC
blocks_on: none
references: []
claim:
  actor: agent:claude-code/27b21f4b
  branch: release/v0.5.0
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-27b21f4b
  commit: 07b6c8b1be8675826d31087bcc0ea34db9364306
  session: null
  claimed_at: 2026-09-30T20:03:50Z
  expires_at: null
archive: null
created_at: 2026-09-30T20:03:46Z
updated_at: 2026-09-30T20:04:29Z
created_by:
  id: agent:claude-code/27b21f4b
  name: ""
updated_by:
  id: agent:claude-code/27b21f4b
  name: ""
extensions: {}
---

## Description

The owner asked on 2026-09-30 to deploy v0.5.0, right after the cut in TKT-01M3RMFD64 (Release v0.5.0: bays and conflicts; notes, tag, archives and image). That request is this deploy's authorization, as TKT-01M3FP11A (Onboarding rollout: upgrade the hosted lake and register machines) requires for each deploy.

### Starting point

- The lake and the workstation agent run v0.4.0 (1740c08) at catalog schema 17 (TKT-01M3NSQJ3K).
- The unit and the web drop-in are byte-identical to the v0.4.0 bundle's copies. v0.5.0 changes neither, and only `deploy/profiles.json.example` changed under `deploy/`.
- v0.5.0 adds migrations 18 to 21 (conflict resolutions, bays, bay scopes, bay rules). It does not touch `internal/normalize`, so nothing needs to be normalized or rebuilt.
- Dashboard sign-ins live in memory. The restart signs everyone out, and the next sign-in carries the groups that bay grants need.

### Approach

This uses the v0.4.0 bundle's script with the versions and schemas changed, plus one new block that checks the bays and conflicts migrations. The owner runs it as root, then the agent upgrades the workstation agent binary in place.

## Acceptance criteria

- [ ] A verified checkpoint of the stopped schema-17 lake exists, taken with v0.4.0
- [ ] The lake runs v0.5.0 at schema 21 with integrity ok, counts preserved, and every session in the default bay
- [ ] Health, auth refusals and the public URL answer after the upgrade, and each web group mapped to viewer or operator holds its default-bay grant
- [ ] The workstation agent runs v0.5.0, stays pinned on the default profile, and its next sync re-uploads nothing
- [ ] The Conflicts page no longer lists the Claude subagent false conflicts migration 18 resolves

## Implementation plan

1. The owner runs `deploy-v0.5.0-JCwhUpMC/operator-deploy.sh` (in the external handoff) as root. It checks every precondition first, then stops the lake and takes a checkpoint with v0.4.0's `serve backup`, which fsck re-hashes. It installs v0.5.0, and serve migrates 17 → 21. The script checks health, the 401s, schema, integrity, counts, the lake id and the public URL. A new block checks that exactly one bay, `default`, holds every session and that grants are read or write only, and prints the group grants and migration 18's conflict resolutions. The script then resumes the agent and waits for a sync.
2. The agent upgrades the workstation agent binary, keeps a copy of the old one and config.json, and checks that it stays pinned on `lake:default`, that `terva-lampi bays` shows `default`, and that the next sync uploads nothing.
3. The owner checks the Conflicts page for the subagent false conflicts that migration 18 resolves.

The script is the v0.4.0 bundle's, which ran on this lake on 2026-09-29, with the versions and schemas changed and the bays block added. The bays block and the journal filter were tested on 2026-09-30 against a scratch lake created by v0.4.0, with a token-file device and a viewer/operator `role_map`, then upgraded by the bundle binary. The unit and web drop-in are byte-identical to the v0.4.0 bundle's copies. The bundle README carries the rollback, which restores only the catalog.
