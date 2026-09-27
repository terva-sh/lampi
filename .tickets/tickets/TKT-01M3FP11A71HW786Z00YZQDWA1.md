---
schema: 3
id: TKT-01M3FP11A71HW786Z00YZQDWA1
title: "Onboarding rollout: upgrade the hosted lake and register machines"
type: task
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area/ops
assignees: []
milestone: null
parent: TKT-01M3FHHBCJ12FXKNTB6Z138F6N
origin: null
dependencies:
  - TKT-01M3FHHBSXHGBJJCA157AA5R16
blocks_on: none
references: []
claim:
  actor: agent:claude-code/e4a47e8c
  branch: rollout/onboarding
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-e4a47e8c
  commit: d9aa2615dcd5a50866929a9b9dbf0151d4df18c5
  session: null
  claimed_at: 2026-09-27T13:15:36Z
  expires_at: null
archive: null
created_at: 2026-09-26T20:20:39Z
updated_at: 2026-09-27T13:17:37Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/e4a47e8c
  name: ""
extensions: {}
---

## Description

Part of the agent onboarding epic. Deploy the onboarding release to the hosted lake, migrate the existing agent, and register the other machines.

The owner authorized merging the children on 2026-09-27, not deploying them. This child needs its own authorization before it starts, the way TKT-01M3FBJM (Review, land and deploy the OIDC dashboard release) had one. Keep host coordinates and credentials in the external handoff, not in the repository.

- Take a protected backup of the lake's data directory before the catalog migration, and keep the previous binary for rollback.
- Upgrade the lake first. Check that the existing agent still syncs as a legacy lake, then run `serve identity` and `serve identity set-url`, and confirm that the key endpoint answers through the TLS proxy.
- Upgrade the workstation agent, and confirm that its state migrated into the `default` lake and that the next sync uploads nothing.
- Register each other machine with a code, and confirm it appears by name and syncs only its allowlisted projects.

## Acceptance criteria

- [ ] A protected backup and rollback binary exist before the lake is upgraded
- [ ] The lake is upgraded, and the existing agent keeps syncing before it is itself upgraded
- [ ] The workstation agent migrates to the default lake with no re-upload
- [ ] Every other machine is registered from a code and syncs its allowlisted projects

## Implementation plan

Authorized by the owner on 2026-09-27 ("promote and work TKT-01M3FP11A so we are deployed"). Same pattern as TKT-01M3FBJM: the agent prepares a checksummed bundle in the external handoff, outside the repo, and the owner runs one operator script as root on the lake host. Host coordinates stay in the handoff.

1. Bundle: a release binary built from main at d9aa261 (CI green), the unchanged checkpoint-backup.py from the dashboard rollout, and copies of the installed unit, web drop-in and proxy route to compare against.
2. Operator script. Preconditions first, with nothing stopped yet: checksums; revision gate; installed binary is the dashboard release; unit, drop-in and route unchanged; lake active; capacity. Then pause the capture agent, stop the lake, fsck with the old binary, and write the verified compressed checkpoint, which holds the catalog copy, CAS and tokens, and the old binary (AC1). Then install the new binary and start it. serve migrates the catalog to schema 6 and makes the lake identity. Check health, 401 on the anonymous APIs, integrity and preserved counts, and the key list locally. Record the public URL with set-url, and check the key list through the TLS proxy.
3. Resume the still-legacy capture agent. Force a sync and wait for a new successful last_sync. serve devices list then shows the legacy token device, bound to the workstation's machine (AC2).
4. The workstation agent upgrade, done as the user: keep a copy of the state and config dirs and of the old binary, install the new binary, and restart the user unit. Check that state moved to lakes/default, and that the next sync uploads 0 with manifests 0 (AC3).
5. Other machines: the owner mints a code per machine on the lake host (serve register), so a code secret never passes through the agent, and runs terva-lampi register on each machine. Check each one appears by name in serve devices list and syncs its allowlisted projects (AC4).

Rollback. The old binary refuses a schema-6 catalog, so a binary swap alone is not a rollback. Before capture resumes: stop the lake, keep the upgraded catalog and its sidecars aside, restore the checkpoint's catalog.db with the old binary, and start. After capture resumes, uploads land in the new catalog, so any rollback needs reconciliation; prefer fixing forward.

## Notes

**agent:claude-code/e4a47e8c** at 2026-09-27T13:17:37Z

Bundle prepared in the external handoff (onboarding-2026-09-27-*), built from main d9aa261 and stamped d9aa261: operator-onboarding.sh (root), README with the three steps and rollback, reused checkpoint-backup.py, copies of the installed unit, drop-in and route. Checked from the user account before handoff: the installed binary is 8adca49; the unit, drop-in and route match; Go's TLS trusts the proxy certificate for the public URL (status 200, chains verified); 79 GiB free. Rollback is a catalog restore from the checkpoint, not a binary swap: 8adca49 refuses a catalog above schema 3. With no profiles.json the lake serves an empty default profile, so new machines need allow rules locally or in a lake profile; the README says so. Code secrets go from the owner's terminal to each machine and never through the agent.
