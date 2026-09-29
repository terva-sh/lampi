---
schema: 3
id: TKT-01M3NJ805R4Q52Z583PENP53BM
title: Deploy v0.3.0 to the internal lake and workstation agent
type: task
status: done
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
  - TKT-01M3NJ8048VR1TEMKBGGSST6KS
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-29T03:10:00Z
updated_at: 2026-09-29T04:15:53Z
created_by:
  id: agent:claude-code/16ebd168
  name: ""
updated_by:
  id: agent:claude-code/16ebd168
  name: ""
extensions: {}
---

## Description

The owner asked on 2026-09-29 to deploy the new release locally. That request is this deploy's authorization, as TKT-01M3FP11A (Onboarding rollout: upgrade the hosted lake and register machines) requires for each deploy.

### Starting point

- The lake and the workstation agent run v0.2.0 (3f71211), catalog schema 15 (TKT-01M3N5R5).
- v0.3.0 adds migration 16 and writes CAS objects and events files as `.zst`, which v0.2.0 cannot read.

### Approach

It follows the owner's decision in TKT-01M3K45MX note 6 and the v0.2.0 bundle's pattern. The bundle goes in the external handoff directory, and the owner runs it as root:

1. Check every precondition before stopping anything.
2. Stop the lake and take a verified checkpoint with the installed v0.2.0's `serve backup` and `serve fsck`. This backup is the only way back.
3. Install v0.3.0 and let serve migrate 15 → 16. The search index rebuilds at start.
4. Queue `serve normalize --all` and SIGHUP serve, so every events file is rewritten compressed.
5. Check health, 401s, schema, integrity, counts, lake id and the public URL, then resume the agent.

The workstation agent is then upgraded in place. `serve compact`, which compresses the objects v0.2.0 stored raw, needs serve stopped and is not in the owner's decision, so it is left for the owner to schedule.

## Acceptance criteria

- [x] A verified checkpoint of the stopped schema-15 lake exists, taken with v0.2.0
- [x] The lake runs v0.3.0 at schema 16 with integrity ok and counts preserved
- [x] Every session is normalized again and the search index is rebuilt
- [x] Health, auth refusals and the public URL answer after the upgrade
- [x] The workstation agent runs v0.3.0 and its next sync re-uploads nothing

## Implementation plan

1. The owner runs deploy-v0.3.0-oQlfxAk8/operator-deploy.sh (in the external handoff) as root. It checks every precondition first, then stops the lake and takes a checkpoint with v0.2.0's serve backup, re-hashed by fsck. It installs v0.3.0, and serve migrates 15 -> 16 and rebuilds search.db. The script checks health, 401s, schema, integrity, counts, lake id and the public URL, queues serve normalize --all with a SIGHUP, and resumes the agent and waits for a sync. It then waits up to an hour for the normalize jobs, and reports the plain events files left and search.db's version. 2. The agent upgrades the workstation agent binary, keeps a copy of the old one, and checks that the next sync uploads nothing. The script is the v0.2.0 bundle's, which ran on this lake on 2026-09-28, with the version and schema lines changed and the normalize and search steps added. The unit and web drop-in are byte-identical to v0.2.0's. serve compact is left out; the owner schedules it.

## Summary

Deployed on 2026-09-29. The owner ran `deploy-v0.3.0-oQlfxAk8/operator-deploy.sh` as root, and it completed on the first run.

- **Checkpoint:** `/var/lib/terva-lampi-pre-v0.3.0-VN3AGVf1`. It holds a `serve backup` of the stopped lake taken by v0.2.0, with a clean fsck and counts matching the live catalog (270 sessions, 8557 artifacts, 8556 provenance rows, schema 15), plus the old binary, the unit, the drop-in and /etc/terva-lampi. serve also kept `migration-backups/catalog-20260929T032211…-v15.db`. v0.2.0 cannot read `.zst`, so this checkpoint is the way back.
- **Lake:** v0.3.0 (a43c5ce), migrated 15 → 16 (migrateProjectReview).
  - Integrity ok, counts preserved, and lake id `lake_u3cpc5lo4dwujlk5il3mpjepai` unchanged.
  - Health, the anonymous 401s and the public URL answer.
- **Normalize and search:** `serve normalize --all` queued all 270 sessions. After the SIGHUP every one was ready, with none failed and no plain `.jsonl` left. search.db was rebuilt at index version 4 and is 349 MiB, down from about 4.9 GB.
- **Agent:** the workstation agent went from v0.2.0 to v0.3.0 after the script finished. Copies of the old binary and config are in `~/.local/state/agent-handoffs/lampi/agent-rollback-v0.3.0-eCe1NigD`. Its first sync sent nothing again (unchanged 157; the 1 upload was a session being written), and status reports `lake_release: v0.3.0`.

Before the agent upgrade, the agent was also adopted with `lakes adopt` (TKT-01M3NMHDWR, "lakes adopt: pin a lake a machine already syncs to, so it takes profiles", on a branch build). It is now pinned and takes the default profile. That switch uploaded 60 sessions the old local rules refused; that ticket records the details. Two normalize failures appeared after it, at 04:06Z. They came from the newly uploaded sessions, not from the upgrade, and are followed up there.

`serve compact`, which compresses blobs stored before v0.3.0, was not run and is left for the owner to schedule.
