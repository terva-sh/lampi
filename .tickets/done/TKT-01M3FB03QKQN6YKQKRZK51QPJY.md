---
schema: 3
id: TKT-01M3FB03QKQN6YKQKRZK51QPJY
title: Prepare the OIDC dashboard deployment package
type: task
status: done
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
claim: null
archive: null
created_at: 2026-09-26T17:07:55Z
updated_at: 2026-09-26T17:14:22Z
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

- [x] Deployment configuration and binary are staged and verified without reading the production client secret.
- [x] Operator runbook covers privileged preflight, backup capacity/encryption, service upgrade, rollback and authentication/agent checks.
- [x] Unfinished release and deployment gates are recorded honestly; production service remains untouched during preparation.

## Implementation plan

Use a unique private directory under the documented external handoff location for host-specific artifacts. Build from a committed revision and record checksums. Preserve the installed service through a minimal systemd drop-in rather than replacing its sandbox. Prepare manual privileged commands so backup, protected secret readability, migration and rollback are reviewable before execution. Check proxy query logging with a synthetic marker, never live callback data.

## Notes

**agent:codex/deploy-prep** at 2026-09-26T17:12:14Z

Prepared a private external deployment bundle with actual operator-supplied OIDC values, a Linux amd64 binary, minimal service drop-in, preserved non-secret config snapshots, staged proxy configuration and operator preflight/backup/install/verification/rollback instructions. Exact web config passed application validation and live HTTPS discovery without reading the client secret. A harmless live probe demonstrated query strings in the current proxy access log. Rather than altering shared global logging, staged router-level accessLogs=false and verified the exact route with an isolated installed-version proxy: protected request served without logging and unrelated control logged. docs/web-dashboard.md records the portable fix. No live configuration was changed. Capacity and encrypted backup destination require operator verification; privileged execution and release review remain gates. The administrator now confirms provider-level denial for the unmapped test account. Full VM-power-loss validation remains separately outstanding.

**agent:codex/deploy-prep** at 2026-09-26T17:14:22Z

Opened release PR https://git.local.sothr.com/terva-sh/lampi/pulls/4 at code/documentation head 2837e192214c78f7142f1e49a594762055e0f301. Remote CI started. Requested terva-review with request ID deployment-prep-2837e19; workflow was accepted despite tea reporting an empty-JSON response error. The review commit status is error: context_limit, stopped before publishing. This is not a passing review or code finding. The release must obtain a review within the reviewer context budget (for example by splitting the change into dependency-ordered reviewable PRs); do not merge/install based on this attempt. Candidate config validation/live discovery, systemd-analyze verify and staged-file checksum verification passed; production remains unchanged.

## Summary

Deployment preparation complete: private staged configuration, revision-stamped candidate, service override, verified proxy logging fix and detailed operator runbook. Artifact integrity is recorded in the external bundle checksums. Production service and proxy remain unchanged. No production secret was read. Actual installation, pre-migration backup, real-user OIDC exchange and release approval are not claimed by this preparation ticket.
