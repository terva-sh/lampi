---
schema: 3
id: TKT-01M3FP11A71HW786Z00YZQDWA1
title: "Onboarding rollout: upgrade the hosted lake and register machines"
type: task
status: draft
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
claim: null
archive: null
created_at: 2026-09-26T20:20:39Z
updated_at: 2026-09-26T22:03:53Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
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
