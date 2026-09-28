---
schema: 3
id: TKT-01M3MC0RXBFWR30R6Q7RY17MJD
title: "Docs: best practices for self-hosting a lake at home"
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/docs
  - area/ops
  - policy
assignees: []
milestone: null
parent: TKT-01M3MC023P4A5H7PTF662QSSM8
origin: null
dependencies:
  - TKT-01M3MC0RTRVC74337N5NBP0FHG
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-28T16:01:57Z
updated_at: 2026-09-28T16:01:58Z
created_by:
  id: agent:claude-code/aa1afd80
  name: ""
updated_by:
  id: agent:claude-code/aa1afd80
  name: ""
extensions: {}
---

## Description

A best-practices section (or `docs/self-hosting.md`, linked from the container guide) for running a lake at home. It's opinionated, and each rule says what goes wrong without it. `docs/policy.md` is the source for the security stance: TLS in front, device tokens, encryption at rest.

### Topics

- **Exposure.** Never publish 8787 on a public interface. Put it behind a TLS reverse proxy on a private container network, or reach it over a VPN or tailnet (Tailscale, WireGuard) instead of opening a port. Clients refuse to send a token to a non-loopback `http://` URL. Set the proxy's request body limit high enough for chunked uploads (see the go-live 32 MiB drill, TKT-01M3D8YXG).
- **Storage.**
  - Keep the lake on a local filesystem. SQLite in WAL mode and `lake.lock` aren't safe on NFS or SMB, so don't put the lake on a NAS share mounted over the network.
  - Use ZFS or btrfs for snapshots if available, but snapshots don't replace `serve backup` (a snapshot of a live `catalog.db` without its WAL may not be consistent unless it's atomic).
  - Encrypt at rest (LUKS, ZFS native encryption): the CAS holds session transcripts in plaintext.
  - Leave room for growth, and use `lampi_*` disk metrics to watch it.
- **One writer.** Run one replica per lake directory. Never run two containers against the same volume.
- **Container hardening.** Non-root user, read-only root filesystem, all capabilities dropped, `no-new-privileges`, and memory and CPU limits sized from the 20K-session drill numbers. Don't mount the Docker socket.
- **Secrets.** Token files and the OIDC client secret as files or Docker secrets, never as environment variables or command arguments. Keep `identity.json` in backups and nowhere else, because it holds the lake's private signing keys.
- **Updates.** Pin a version or digest. Let Renovate open the bump rather than Watchtower pulling `latest`, because a release can migrate the catalog and rollback needs a backup.
- **Backups.** Keep backups off the host, on a schedule, and run a restore drill (see the backups ticket).
- **Monitoring.** Scrape metrics and alert on the essentials (see the monitoring ticket).
- **Time and clocks.** Registration codes expire, OIDC checks token times, and head-update buckets are UTC, so the host needs working NTP.

## Acceptance criteria

- [ ] Each practice states what goes wrong without it
- [ ] Exposure, storage, single writer, hardening, secrets, updates, backups, monitoring and clocks are covered
- [ ] The guidance agrees with docs/policy.md
