---
schema: 3
id: TKT-01M3N5R5EJ358J0NVY9EM8VYMX
title: Deploy v0.2.0 to the internal lake and workstation agent
type: task
status: in-progress
status_reason: null
priority: high
due_on: null
labels:
  - area/ops
assignees: []
milestone: v0.2.0
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim:
  actor: agent:claude-code/aa1afd80
  branch: tickets/v0.2.0-released
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-aa1afd80
  commit: 6e3272db1683faec1aca15d9f5aede201108dd1f
  session: null
  claimed_at: 2026-09-28T23:31:38Z
  expires_at: null
archive: null
created_at: 2026-09-28T23:31:38Z
updated_at: 2026-09-28T23:33:41Z
created_by:
  id: agent:claude-code/aa1afd80
  name: ""
updated_by:
  id: agent:claude-code/aa1afd80
  name: ""
extensions: {}
---

## Description

The owner asked on 2026-09-28 to deploy v0.2.0 to the internal lake. That request is this deploy's authorization, as TKT-01M3FP11A (Onboarding rollout: upgrade the hosted lake and register machines) requires for each deploy.

### Starting point

- The lake and the workstation agent run `terva-lampi 0.1.4-dev (5b41022)`, the #81 merge. That build writes catalog schema 14. No ticket records its deploy.
- v0.2.0 (3f71211) adds one migration, 14 → 15 (device inventories).
- The systemd unit and the web drop-in need no change. v0.2.0 still takes `--web-config` and `--metrics-addr`, and the loopback bind needs no `--behind-proxy`.

### Approach

A bundle in the external handoff directory, `deploy-v0.2.0-*`, run by the owner as root. It follows the onboarding bundle's pattern: check every precondition before stopping anything, take a verified checkpoint, install, check, and resume.

The checkpoint uses the installed binary's own `serve backup` on the stopped lake, instead of the onboarding bundle's `checkpoint-backup.py`. `serve backup` copies the catalog, CAS, identity, audit log and tokens, and fsck then re-hashes the copy. The old binary takes the backup because v0.2.0 refuses lockless admin commands on an older schema. serve also copies the catalog into `migration-backups/` before migrating.

After the lake, the workstation agent is upgraded to v0.2.0 in place. The remote devices can then use `terva-lampi self-update`.

Host coordinates stay in the bundle, outside the repository.

## Acceptance criteria

- [ ] A verified checkpoint of the stopped schema-14 lake exists, with the old binary
- [ ] The lake runs v0.2.0 at schema 15 with integrity ok and counts preserved
- [ ] Health, auth refusals and the public URL answer after the upgrade
- [ ] The workstation agent runs v0.2.0 and its next sync re-uploads nothing

## Implementation plan

1. The owner runs deploy-v0.2.0-GaljwMxa/operator-deploy.sh (in the external handoff) as root. It checks every precondition first, takes a checkpoint of the stopped lake with the installed binary's serve backup and re-hashes it with fsck, installs v0.2.0, lets serve migrate 14 → 15, checks health, 401s, schema, integrity, counts, lake id and the public URL, then resumes the agent and waits for a sync. 2. The agent upgrades the workstation agent binary, keeps a copy of the old one, and checks that the next sync uploads nothing. Rehearsal on 2026-09-28: a scratch lake seeded by v0.1.2 and migrated to 14 by a 5b41022 build went through every command the script runs. The rehearsal also caught a lake-id grep that matched the 'lake_id' label; that is fixed in the bundle.
