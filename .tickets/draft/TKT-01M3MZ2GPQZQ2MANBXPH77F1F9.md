---
schema: 3
id: TKT-01M3MZ2GPQZQ2MANBXPH77F1F9
title: "Ops: serve backup --every for an in-container backup schedule"
type: task
status: draft
status_reason: null
priority: low
due_on: null
labels:
  - area/ops
  - area/server
assignees: []
milestone: null
parent: TKT-01M3MC023P4A5H7PTF662QSSM8
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-28T21:34:57Z
updated_at: 2026-09-28T21:34:57Z
created_by:
  id: agent:claude-code/aa1afd80
  name: ""
updated_by:
  id: agent:claude-code/aa1afd80
  name: ""
extensions: {}
---

## Description

`serve backup` runs once and exits. Container operators schedule it from the host with a systemd timer or cron (TKT-01M3MC0S0, "Ops: scheduled and off-host lake backups for container deployments"). The image has no shell, so the only in-container option would be a scheduler sidecar, and one that mounts the Docker socket is ruled out because the socket grants root on the host.

`serve backup --every DURATION` would run the backup on an interval inside a long-lived process: a second compose service sharing the lake volume, or a Kubernetes sidecar where a `CronJob` can't mount a `ReadWriteOnce` volume beside the running pod.

### What it needs

- A loop that runs one backup, sleeps until the next interval, and exits cleanly on SIGTERM, including partway through a copy. A partial copy must never be followed by `--prune`; that already holds, because prune runs only after every copy succeeded.
- No overlapping runs, and a skipped interval is logged rather than queued.
- A failed run is logged and the loop keeps going; the process exits only on a signal.
- Status a monitor can read: the time of the last successful backup, as a file in the backup directory or as a metric, so an alert can fire when backups stop (see TKT-01M3MC0S3).
- An optional post-backup hook is out of scope. The off-host step (restic) stays on the host timer or in its own container.

Estimated at about a day, including tests with a fake clock and the compose example.

## Acceptance criteria

- [ ] serve backup --every runs on an interval and exits cleanly on SIGTERM
- [ ] Runs never overlap, and a failed run does not stop the loop
- [ ] The last successful backup time is readable by a monitor
- [ ] The compose example shows it as an optional second service
