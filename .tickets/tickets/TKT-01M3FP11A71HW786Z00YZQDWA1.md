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
updated_at: 2026-09-28T14:45:12Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/2cf53976
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

- [x] A protected backup and rollback binary exist before the lake is upgraded
- [x] The lake is upgraded, and the existing agent keeps syncing before it is itself upgraded
- [x] The workstation agent migrates to the default lake with no re-upload
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

**agent:claude-code/cd41c9ac** at 2026-09-26T22:03:53Z

### The recall release ships with this rollout (PR #12, merged as fcaec88)

main now includes the transcript viewer, search and copy-out from the recall epic TKT-01M3FPP3 (Session recall: one query surface for the web UI and an MCP server). On 2026-09-26 the owner chose to hold its deployment until this rollout instead of deploying it separately. This rollout therefore deploys both. What it adds to the hosted lake:

- **search.db, a new derived file in the data directory.** serve creates it only with --web-config, which the hosted lake uses. On first start it indexes every ready session in the background while serving; there is no migration step. Size is about 2 to 3 times the indexed text: measured 116 MiB for 50 MiB. The data directory's free space was 104 GiB on 2026-09-26. It holds no secrets; `serve backup` skips it and a restore rebuilds it. `serve purge` removes a session's rows. To rebuild: stop serve, delete search.db, search.db-wal and search.db-shm, then start serve.
- **No catalog change from recall.** The schema bump to 4 is lake identity's alone.
- **New viewer routes behind the existing OIDC guard:** /sessions/{uid}/transcript, /search, /sessions/{uid}/excerpt, and /api/web/v1/{sessions/{uid}/events, search, sessions/{uid}/excerpt}. No proxy change is needed. Keep router access logging off as before: search queries travel in the URL.

Checks after the upgrade, before resuming capture:
1. Anonymous GET /api/web/v1/search?q=abc returns 401.
2. Signed in, /search shows "Searching N of M ready sessions" and N reaches M within minutes.
3. A transcript opens.
4. A search hit opens its event highlighted.
5. Copy as text puts an excerpt with an absolute https link on the clipboard.

Rollback is unaffected by recall: search.db can be left in place or deleted. The pre-identity binary refuses the schema-4 catalog regardless of recall, so the catalog checkpoint the identity migration needs covers this release too.

**agent:claude-code/e4a47e8c** at 2026-09-27T13:17:37Z

Bundle prepared in the external handoff (onboarding-2026-09-27-*), built from main d9aa261 and stamped d9aa261: operator-onboarding.sh (root), README with the three steps and rollback, reused checkpoint-backup.py, copies of the installed unit, drop-in and route. Checked from the user account before handoff: the installed binary is 8adca49; the unit, drop-in and route match; Go's TLS trusts the proxy certificate for the public URL (status 200, chains verified); 79 GiB free. Rollback is a catalog restore from the checkpoint, not a binary swap: 8adca49 refuses a catalog above schema 3. With no profiles.json the lake serves an empty default profile, so new machines need allow rules locally or in a lake profile; the README says so. Code secrets go from the owner's terminal to each machine and never through the agent.

**agent:claude-code/e4a47e8c** at 2026-09-27T14:12:36Z

Lake upgraded by the owner on 2026-09-27 with the bundle's operator script (installed release d9aa261).

- AC1: a verified checkpoint in /var/lib/terva-lampi-pre-onboarding-* (6.78 GB compressed; catalog schema 3 with 84 sessions and 3150 artifacts and provenance rows), holding the old 8adca49 binary for rollback.
- The catalog migrated to schema 6 with integrity ok and counts preserved. serve created the lake identity (lake_u3cpc5lo4dwujlk5il3mpjepai, key 5f7fac541ec9bf33). set-url recorded the public URL after checking that it serves this lake. The key list and health answer through the TLS proxy.
- AC2: the still-legacy agent (705a2b7) completed a sync against the upgraded lake (checked 1, uploaded 1). Its token-file device, token-1, bound to the workstation's machine id, the same id as in machine.json.
- AC3: the workstation agent was upgraded in place. Before that, copies of its config, state and old binary were taken to a private directory under ~/.local/state (outside every repo) for rollback. On start it logged "moved sync state to .../lakes/default"; the machine id is unchanged; the legacy files are gone from the top level. Its first passes counted 83 files unchanged and uploaded only one: this session's own transcript, which is still growing. A later pass uploaded nothing (checked 0, unchanged 84). Nothing was re-uploaded.

The refused count, 78, is the agent's allowlist refusals and was the same before the upgrade; quarantined stayed at 6. The workstation's entry stays a legacy one: there is no lake_id pin and no profile fetch. Moving it to a pinned, registered entry would take register --replace and is not needed for this ticket.

**agent:claude-code/e4a47e8c** at 2026-09-27T14:22:52Z

AC4 is in the owner's hands: they will register NeoT, their main laptop and the servers kobal and shai. Cross-built binaries of d9aa261 for linux and darwin (amd64, arm64), with checksums, are in the rollout bundle's machines/ directory outside the repository. A new machine uploads nothing until it has allow rules; follow-ups filed as TKT-01M3HKYFE1 (Allow and deny rules by git remote prefix), TKT-01M3HKYFF5 (Report which projects the allowlist refuses) and TKT-01M3HKYFCW (Operator re-normalization for stale and failed sessions).

**agent:claude-code/2cf53976** at 2026-09-28T14:45:12Z

2026-09-28: the first remote device (tehbeast, dev_5367lazjlql5ck435rvffchi44) refused all 221 sessions because the lake had no default profile allow rules. A default profile copied from brokkr's local allowlist (28 allow rules, agent debounce 5s/30s, version sha256:8427a42989cbc15f) was prepared for /var/lib/terva-lampi/profiles.json; installing it needs sudo on brokkr. Follow-up design is epic TKT-01M3M7KB32 (Lake-managed agent config: dashboard editing, push, inventory).
