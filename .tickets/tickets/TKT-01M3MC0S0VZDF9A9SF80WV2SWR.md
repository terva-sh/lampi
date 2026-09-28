---
schema: 3
id: TKT-01M3MC0S0VZDF9A9SF80WV2SWR
title: "Ops: scheduled and off-host lake backups for container deployments"
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/ops
  - area/docs
assignees: []
milestone: null
parent: TKT-01M3MC023P4A5H7PTF662QSSM8
origin: null
dependencies:
  - TKT-01M3MC0QVMMW0TQ6RGAYDZEF82
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-28T16:01:57Z
updated_at: 2026-09-28T16:03:33Z
created_by:
  id: agent:claude-code/aa1afd80
  name: ""
updated_by:
  id: agent:claude-code/aa1afd80
  name: ""
extensions: {}
---

## Description

`serve backup --out DIR` copies the catalog (with `VACUUM INTO`), the CAS, `identity.json`, `audit.jsonl`, and the token file. It runs while `serve` runs, and a second run into the same directory copies only what changed. `docs/vps-bringup.md` covers backup and a restore drill for the systemd setup. TKT-01M3FBQX ("Add optional compressed age-encrypted backups and restore") adds encrypted archives.

Container operators need the same thing as a scheduled job:

- **Scheduling.** A documented way to run `serve backup` on a schedule next to the lake container:
  - a second service in the compose example that shares the volume and runs the same image with an interval loop or a supercronic-style scheduler (the distroless image has no shell, so pick an approach that doesn't need one);
  - or a host cron entry running `docker exec`;
  - and, for Kubernetes, a `CronJob`, which can't share a `ReadWriteOnce` volume with a running pod on another node, so say so.
- **Off-host copies.** Getting the backup directory off the host (restic, rclone, borg) without writing a tool of our own. The backup is plaintext until TKT-01M3FBQX lands, so the target has to be encrypted.
- **Restore in a container.** Stop the service, restore into a fresh volume, start the same tag that took the backup (or a newer one, which migrates), and check the result with `serve fsck` and `status` from an agent. Make this a documented restore drill for containers, and automate it if the go-live restore drill (TKT-01M3D8YXD) can be pointed at the image.
- **Retention.** Advice on how many backups to keep, and a warning that `serve purge` doesn't reach older backups.

Settle the dependency on TKT-01M3FBQX: the container docs should use encrypted backups once they exist, but this ticket shouldn't wait for them.

## Acceptance criteria

- [ ] A documented schedule for serve backup works with the shell-less image
- [ ] Off-host copy to an encrypted target is documented
- [ ] A container restore drill was run and documented
- [ ] The relationship to TKT-01M3FBQX is recorded
