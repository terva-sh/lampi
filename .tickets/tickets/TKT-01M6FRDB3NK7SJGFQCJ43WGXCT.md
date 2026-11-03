---
schema: 3
id: TKT-01M6FRDB3NK7SJGFQCJ43WGXCT
title: Deploy v0.12.0 to the dogfooding lake
type: task
status: in-progress
status_reason: null
priority: high
due_on: null
labels:
  - area/ops
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim:
  actor: agent:claude-code/t3-0270c158
  branch: tickets/deploy-v0.12.0
  worktree: /home/sothr/.t3/worktrees/lampi/t3-0270c158
  commit: 6d82e2102f20b3e9a1262705426e8a45745c5f28
  session: null
  claimed_at: 2026-11-03T03:52:39Z
  expires_at: null
archive: null
created_at: 2026-11-03T03:49:25Z
updated_at: 2026-11-03T03:52:39Z
created_by:
  id: agent:claude-code/t3-0270c158
  name: ""
updated_by:
  id: agent:claude-code/t3-0270c158
  name: ""
extensions: {}
---

## Description

The owner asked on 2026-11-02 to prepare the deployment of v0.12.0 to the dogfooding lake, right after it was published (TKT-01M6FK6T3EBN2V08Q19M8PMPB9). That request authorizes preparing the deployment. The owner runs the root step, because this session has no noninteractive sudo. It does not authorize upgrading the workstation capture agent, changing web.json, grants, bays or profiles, or running the Vuoro migration (TKT-01M6F5A01MX2ZCP9J7M3GGD7M0).

Live state before: the brokkr systemd lake (terva-lampi-serve.service, /var/lib/terva-lampi, loopback 8787, public lampi.brokkr.local.sothr.com) runs v0.11.1 (527d5c368339). The user capture agent also runs v0.11.1. Host coordinates and the bundle stay in the external handoff under the agent-handoffs lampi directory, as for TKT-01M4ERWGJ894HBPWJWSC3KSYR3.

### What differs from earlier deployments

v0.12.0 migrates the catalog from schema 21 to 23, so this is not a binary swap. The script runs `serve migrate` explicitly on the stopped lake after the verified checkpoint. That prints the path of the v21 backup and fails before any start if a step fails. An automatic rollback after the migration must restore that v21 catalog together with the v0.11.1 binary. The local agent is still paused at that point, so only uploads from other devices in the seconds between the start and a failed check could be lost from the catalog.

### Alternatives

Letting `serve` migrate at start (as the docs allow) was rejected: the migration would then run under systemd's start timeout, and a failure would appear only as a failed start. An explicit step gives a named backup and a check of schema 23 before anything listens. Upgrading the agent in the same run was rejected to keep one variable per step: the agent needs no upgrade to keep uploading, and doing it separately keeps the rollback a lake-only operation.

## Acceptance criteria

- [x] A checksummed v0.12.0 bundle and operator script are ready, with checkpoint, migration-backup and rollback gates, rehearsed on synthetic data
- [ ] The dogfooding lake runs v0.12.0 at schema 23 with identity, counts, health, auth guards and configuration preserved
- [ ] The public site responds, capture resumes, and the outcome is recorded

## Implementation plan

Adapt the 92c2dbe root-run operator script to a migrating release. Preconditions are unchanged except that migrate --check must report 21 -> 23 with 2 pending, and the space check adds one catalog copy. After the verified v0.11.1 checkpoint, run serve migrate on the stopped lake, record the v21 backup it names, and check schema 21 in the backup and 23 in the catalog before the start. After the start, also check anonymous 401s on the new read routes, bay and membership counts, and filled project columns. Sessions may only grow, since other devices upload once serve listens. Automatic rollback before capture resumes restores the v21 catalog, removes -wal/-shm and reinstalls v0.11.1. After the checks pass, the script never rolls back on its own. Rehearse success, forced failure and refusal against a fake root, then hand the owner one sudo command, because there is no noninteractive sudo.

## Notes

**agent:claude-code/t3-0270c158** at 2026-11-03T03:52:39Z

Bundle ready in the external handoff (deploy-v0.12.0-* under the agent-handoffs lampi directory): the GitHub v0.12.0 linux_amd64 binary, verified against the release checksums.txt and equal to the archive's binary, plus operator-deploy.sh, README.md with a manual rollback, the unit and drop-in copies, and SHA256SUMS. bash -n passes. Live state just before handoff: lake and user agent both v0.11.1 and active, units equal the copies, loopback and public health 200, 127G free. Rehearsal against a fake root seeded by a real v0.11.1 sync, with a systemctl shim (rehearse-deploy.sh in the release-v0120 scratch dir): success reached v0.12.0 at schema 23 with counts and identity preserved and project columns filled on 2 of 2 sessions, and a v0.11.1 agent then synced unchanged 2, uploaded 0. A forced failure after start restored the v21 migration backup and v0.11.1, and the lake was healthy at schema 21 (status rolled-back). A wrong installed version was refused with nothing changed. Not rehearsed: the agent pause and resume, because the fake agent was inactive; that code is unchanged from the 92c2dbe deploy. Waiting for the owner to run the root command.
