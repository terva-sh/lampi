---
schema: 3
id: TKT-01M3FBJMET80690HK2CJQD609M
title: Review, land and deploy the OIDC dashboard release
type: task
status: in-progress
status_reason: null
priority: high
due_on: null
labels:
  - area/ops
  - area/auth
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim:
  actor: agent:codex/deploy
  branch: t3code/web-session-lake-ui
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-8b6762d2
  commit: 88b36ad1fdabba1da0b4262d1d8fced625200795
  session: null
  claimed_at: 2026-09-26T17:18:02Z
  expires_at: null
archive: null
created_at: 2026-09-26T17:18:02Z
updated_at: 2026-09-26T17:18:02Z
created_by:
  id: agent:codex/deploy
  name: ""
updated_by:
  id: agent:codex/deploy
  name: ""
extensions: {}
---

## Description

Land the dashboard release after successful CI and model review, then upgrade the existing hosted lake with a protected pre-migration backup and verify real OIDC and agent behavior. Keep host coordinates and credentials outside the repository. The owner explicitly authorized merging and deployment. No shared-host power-off is authorized by this task.

## Acceptance criteria

- [ ] All release code receives a published model review; findings are resolved or dispositioned while PRs remain open; CI passes and reviewed changes merge.
- [ ] A protected pre-migration backup, rollback binary/config and service-account configuration checks are completed before installation.
- [ ] The reviewed binary and OIDC/proxy configuration are installed; health, allowed/denied login, logout and device ingestion are verified.

## Implementation plan

Reduce review scope by landing the existing dependency-ordered OIDC/catalog foundation commits first, then review and land the remaining dashboard and operational changes through the original PR. Preserve history without force pushes. Read each published review and verify fixes before merging; synchronize both forges. Rebuild from the reviewed revision, then follow the external deployment runbook only after privileged execution and protected backup storage are available. Record actual verification separately from preparation.
