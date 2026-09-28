---
schema: 3
id: TKT-01M3MC023P4A5H7PTF662QSSM8
title: "Self-hosted lake: container image, registry, and operations"
type: epic
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area/ops
  - area/server
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim:
  actor: agent:claude-code/aa1afd80
  branch: t3code/review-lampi-lake-containerization
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-aa1afd80
  commit: 8242424255a21e030b5e52e9f418a837b44c94b1
  session: null
  claimed_at: 2026-09-28T16:03:40Z
  expires_at: null
archive: null
created_at: 2026-09-28T16:01:34Z
updated_at: 2026-09-28T16:03:40Z
created_by:
  id: agent:claude-code/aa1afd80
  name: ""
updated_by:
  id: agent:claude-code/aa1afd80
  name: ""
extensions: {}
---

## Description

Today a self-hosted lake means a VPS or home server running the `terva-lampi serve` systemd unit from `deploy/systemd/`, set up by hand following `docs/vps-bringup.md`. People running a home server usually run services as containers under Docker, Podman, Compose, or a small Kubernetes such as k3s, and expect a published image, a compose file, and a page telling them how to run it safely.

The only image in the tree is `e2e/Dockerfile`. It builds `terva-lampi:synthetic` for the container smoke tests (PRs #49 to #52). It binds `serve` to loopback inside the container with no token file, is never pushed, and is not meant to be run for real.

This epic ships a production image and the operations support around it:

- A production Containerfile for `serve`.
- A health probe that works in a shell-less image.
- Catalog migrations and upgrades that are safe under a container restart policy.
- A GitHub Container Registry package for `terva-sh/lampi`, and release CI that publishes the image.
- Documentation for running the image, and a best-practices guide.
- Backups, monitoring, and an optional Kubernetes manifest for self-hosters.

### Scope

- Targets are `linux/amd64` and `linux/arm64` only. No 32-bit ARM, no Windows or macOS images. Desktop users run the agent from the release archives, not from a container.
- The image runs the lake (`serve` and its admin subcommands). Running the agent in a container is out of scope, because it has to read harness homes on the host.
- The systemd deployment stays supported. The container path adds to it and does not replace it.

### Facts the children rely on

- The catalog migrates itself when `serve` opens it: `internal/catalog/catalog.go` runs each step in `migrations` above `PRAGMA user_version`, one transaction per step. A binary refuses a catalog whose version is newer than it knows about, so rolling back an image after a migration fails at start.
- `serve` holds `lake.lock`, so only one `serve` can run on a lake directory. `serve backup`, `serve devices`, and `serve register` run while `serve` runs. `serve compact` and `serve fsck --repair` need `serve` stopped.
- On SIGTERM, `serve` waits up to 20s for requests in flight and up to 30s for the normalize queue. Docker's default stop timeout is 10s.
- `serve` refuses a non-loopback `--addr` without `--token-file`, and warns when it has one. `--metrics-addr` refuses a non-loopback address without `--metrics-public`. In a container, the listener has to be on the container's own network interface to be reachable from outside the container.
- `serve` reads flags only. The `LAMPI_SERVE_*` names in `deploy/systemd/serve.env.example` are substituted by systemd, and `serve` does not read them.
- Release archives already build `linux/amd64` and `linux/arm64` with `CGO_ENABLED=0` through goreleaser (TKT-01M3HS0F, "Release archives on GitHub and a curl-able install.sh").

## Acceptance criteria

- [ ] A published multi-arch image runs a lake on linux/amd64 and linux/arm64
- [ ] Upgrading the image migrates the catalog safely, with a documented rollback
- [ ] An operator can go from nothing to a TLS-fronted, backed-up, monitored lake by following the docs
