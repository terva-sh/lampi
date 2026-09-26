---
schema: 3
id: TKT-01M3FB03QKQN6YKQKRZK51QPJY
title: Prepare the OIDC dashboard deployment package
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
  actor: agent:codex/deploy-prep
  branch: t3code/web-session-lake-ui
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-8b6762d2
  commit: 64f745b77bf83fba4d6a15ed75f100b16d39a6ee
  session: null
  claimed_at: 2026-09-26T17:07:55Z
  expires_at: null
archive: null
created_at: 2026-09-26T17:07:55Z
updated_at: 2026-09-26T17:07:55Z
created_by:
  id: agent:codex/deploy-prep
  name: ""
updated_by:
  id: agent:codex/deploy-prep
  name: ""
extensions: {}
---

## Description

Prepare a reviewable deployment package from the validated dashboard revision, with exact operator-supplied OIDC configuration kept outside the repository, checksummed binary, service override, preflight/backup instructions, explicit migration rollback and post-install verification. This ticket prepares artifacts; it does not assert a production upgrade or completed real-user smoke.

## Acceptance criteria

- [ ] Deployment configuration and binary are staged and verified without reading the production client secret.
- [ ] Operator runbook covers privileged preflight, backup capacity/encryption, service upgrade, rollback and authentication/agent checks.
- [ ] Unfinished release and deployment gates are recorded honestly; production service remains untouched during preparation.

## Implementation plan

Use a unique private directory under the documented external handoff location for host-specific artifacts. Build from a committed revision and record checksums. Preserve the installed service through a minimal systemd drop-in rather than replacing its sandbox. Prepare manual privileged commands so backup, protected secret readability, migration and rollback are reviewable before execution. Check proxy query logging with a synthetic marker, never live callback data.
