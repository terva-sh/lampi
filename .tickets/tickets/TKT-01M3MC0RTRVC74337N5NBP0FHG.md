---
schema: 3
id: TKT-01M3MC0RTRVC74337N5NBP0FHG
title: "Docs: run the lake from the container image, with a compose example"
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/docs
  - area/ops
assignees: []
milestone: null
parent: TKT-01M3MC023P4A5H7PTF662QSSM8
origin: null
dependencies:
  - TKT-01M3MC0QVMMW0TQ6RGAYDZEF82
  - TKT-01M3MC0RA026HX6ZKSKG89F5NA
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

Write `docs/container.md`: how to run the lake from the published image. The audience is someone with a home server, Docker or Podman, and a reverse proxy they already run. It parallels `docs/vps-bringup.md`, which stays the systemd guide, and links to it for anything that isn't container-specific.

### Contents

- **Quick start.** `docker run` with a named volume or bind mount for `/lake`, a token directory, a published port or proxy network, and a pinned tag.
- **Compose.** `deploy/compose/compose.yaml` (and a Podman quadlet if it's cheap) as a checked-in example with:
  - `stop_grace_period: 60s` (drain is 20s plus 30s);
  - `restart: unless-stopped`;
  - the healthcheck with a `start_period` long enough for a migration;
  - `read_only: true`, `cap_drop: [ALL]`, `security_opt: [no-new-privileges:true]`, `user: 65532:65532`;
  - secrets for the token and OIDC client secret files;
  - an optional reverse proxy service (Caddy or Traefik labels) that terminates TLS.

  Following the repository rule, no real hostname: use `lake.example.com`.
- **First start.** What gets created in `/lake` (`identity.json`, `catalog.db`, `cas/`, and so on), how to fix ownership for UID 65532 on a bind mount, and how to set the lake URL with `serve identity set-url`.
- **Devices.** `docker exec <c> terva-lampi serve register --name NAME` to mint a code, then run `terva-lampi register` on each machine. Also `serve devices` and reloading with `docker kill -s HUP`.
- **Optional features.** The OIDC dashboard (`--web-config` from a mounted file) and metrics (`--metrics-addr` plus `--metrics-public` on a private network only).
- **Maintenance that needs `serve` stopped.** `serve compact` and `serve fsck --repair` take `lake.lock`: stop the service, then `docker compose run --rm lampi serve compact`, then start it again.
- **Upgrades and rollback.** Pin a tag. Read the release notes for schema changes. Back up first. Say what happens on start (migration and re-normalize logs). The one supported rollback path comes from the migrations ticket.
- **Logs.** One line per request on stderr, `docker logs`, no Authorization header.
- **Verifying the image.** How to check the signature, SBOM, and provenance, if the CI ticket ships them.

Link it from `docs/README.md`, the README install section, and `deploy/README.md`. Run the technical-writing standard on it.

## Acceptance criteria

- [ ] docs/container.md covers quick start, first start, devices, optional features, stopped-lake maintenance, upgrades and logs
- [ ] A checked-in compose example sets the stop grace period, healthcheck, hardening options and secrets
- [ ] The guide is linked from docs/README.md, the README and deploy/README.md
- [ ] Every command in the guide was run against a real container
