---
schema: 3
id: TKT-01M3MC0S0VZDF9A9SF80WV2SWR
title: "Ops: scheduled and off-host lake backups for container deployments"
type: task
status: in-progress
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
claim:
  actor: agent:claude-code/aa1afd80
  branch: self-host/backups
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-aa1afd80
  commit: b3fe43033b0f2e879ffd0c78d4490b5249c911ab
  session: null
  claimed_at: 2026-09-28T21:35:10Z
  expires_at: null
archive: null
created_at: 2026-09-28T16:01:57Z
updated_at: 2026-09-28T21:35:10Z
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

## Notes

**agent:claude-code/aa1afd80** at 2026-09-28T20:35:33Z

### Decisions (owner, 2026-09-28)

- **Scheduling: a host timer.** A systemd timer (or cron) on the host runs `docker compose exec -T lampi terva-lampi serve backup ...`, then the restic step, in that order and never overlapping. A scheduler container that mounts the Docker socket is ruled out, because the socket grants root on the host. Whether `serve backup --every` is also built is still open; see the note on its cost.
- **Prune: build `serve backup --prune`.** `cas.Store.Backup` only adds to the backup directory. So `serve purge` never reaches a backup, and the space `serve compact` saves never does either: the backup keeps every object the lake ever held. `--prune` removes from the backup every object that the new catalog snapshot doesn't reference, and only after a copy that fully succeeded. Old restic snapshots still need `restic forget`, and the docs say so after a purge.
- **Off-host: restic as the documented example,** pointed at the backup directory and run after `serve backup`. It encrypts before data leaves the host, keeps deduplicated snapshots, and runs `forget --prune` with a suggested 7 daily, 4 weekly, 6 monthly. `rclone crypt` and Borg get a mention.
- **TKT-01M3FBQX (Add optional compressed age-encrypted backups and restore) is optional, not a dependency.** Restic covers encryption and compression on this path. Age archives stay useful for anyone who wants encrypted archives without a backup tool.

### Open before the restore drill

The derived files (`normalized/`, `parquet/`, `search.db`) aren't in a backup. The systemd drill rebuilds them with `export` before `serve` starts. Check whether `serve` or `serve normalize --all` rebuilds them in a container, where `export` needs a writable output path.

**agent:claude-code/aa1afd80** at 2026-09-28T21:35:07Z

### serve backup --prune built

- `internal/api/live.go` holds `liveDigests`, the reachability walk `serve compact` used inline, so compact and prune share one rule for what a lake needs. The walk starts from the catalog's referenced digests and each session's last manifest, and follows prefix records and chunk lists. Compact's dry run still passes its planned folds.
- `api.PruneBackup(dir)` opens the backup's own `catalog.db` read-only, walks it against `dir/cas`, and removes every object and logical entry outside that set. `serve backup --prune` calls it only after the catalog, CAS, identity, audit and token copies all succeeded.
- A logical entry that doesn't parse stops the prune before anything is removed, the same rule compact follows. A digest the catalog names and the CAS lacks is reported on stderr, but it doesn't stop the prune, because removing other things can't make it worse.
- The alternative was to prune by comparing against the lake's CAS, removing from the backup whatever the lake no longer holds. It lost because it reads the live lake a second time, after the snapshot, and so would race uploads and compaction. Pruning against the backup's own catalog is self-contained, and it gives the same answer run later against a restored copy.
- Compact keeps a cutoff so it doesn't remove objects uploaded but not yet named. The backup has no in-flight uploads, so prune doesn't need one.

`--every` is filed separately as draft TKT-01M3MZ2G (Ops: serve backup --every for an in-container backup schedule), at the owner's request.
