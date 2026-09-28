---
schema: 3
id: TKT-01M3N5R5EJ358J0NVY9EM8VYMX
title: Deploy v0.2.0 to the internal lake and workstation agent
type: task
status: done
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
claim: null
archive: null
created_at: 2026-09-28T23:31:38Z
updated_at: 2026-09-28T23:40:49Z
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

- [x] A verified checkpoint of the stopped schema-14 lake exists, with the old binary
- [x] The lake runs v0.2.0 at schema 15 with integrity ok and counts preserved
- [x] Health, auth refusals and the public URL answer after the upgrade
- [x] The workstation agent runs v0.2.0 and its next sync re-uploads nothing

## Implementation plan

1. The owner runs deploy-v0.2.0-GaljwMxa/operator-deploy.sh (in the external handoff) as root. It checks every precondition first, takes a checkpoint of the stopped lake with the installed binary's serve backup and re-hashes it with fsck, installs v0.2.0, lets serve migrate 14 → 15, checks health, 401s, schema, integrity, counts, lake id and the public URL, then resumes the agent and waits for a sync. 2. The agent upgrades the workstation agent binary, keeps a copy of the old one, and checks that the next sync uploads nothing. Rehearsal on 2026-09-28: a scratch lake seeded by v0.1.2 and migrated to 14 by a 5b41022 build went through every command the script runs. The rehearsal also caught a lake-id grep that matched the 'lake_id' label; that is fixed in the bundle.

## Summary

Deployed on 2026-09-28. The owner ran the bundle's `operator-deploy.sh` as root. The first attempt stopped in a precondition, with nothing changed: `terva-lampi` could not execute the new binary inside the bundle under /home/sothr. The script now stages the binary as a root-owned temp file in /usr/local/bin, which the install renames into place. The second run succeeded.

- **Checkpoint:** `/var/lib/terva-lampi-pre-v0.2.0-uvNbxf1P`. It holds a `serve backup` of the stopped lake taken by 5b41022, with a clean fsck and counts matching the live catalog (257 sessions, 8050 artifacts, 8049 provenance rows, schema 14), plus the old binary, the unit, the drop-in and /etc/terva-lampi. serve also kept `migration-backups/catalog-20260928T233951…-v14.db`.
- **Lake:** v0.2.0 (3f71211), migrated 14 → 15.
  - Integrity ok, counts preserved.
  - Lake id `lake_u3cpc5lo4dwujlk5il3mpjepai` unchanged.
  - The public URL answers, and the anonymous APIs return 401.
  - Six devices are active (kobal, pherocity14, pherocity16, shai, tehbeast, token-1), and the `default` profile is at revision 1 from `serve profiles import`. That answers TKT-01M3FP11A's note 6.
- **Agent:** the workstation agent went from 5b41022 to v0.2.0. Before that, copies of its binary, config and state were taken to `~/.local/state/agent-handoffs/lampi/agent-rollback-v0.2.0-o99O6ZLi`. Its first sync uploaded nothing (unchanged 98), and `status` reports `lake_release: v0.2.0` with normalization caught up.

The remote devices can upgrade with `terva-lampi self-update`.
