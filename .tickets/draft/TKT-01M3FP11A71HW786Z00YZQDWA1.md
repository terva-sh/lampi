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
updated_at: 2026-09-26T20:20:39Z
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
