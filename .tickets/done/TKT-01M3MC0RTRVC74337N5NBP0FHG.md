---
schema: 3
id: TKT-01M3MC0RTRVC74337N5NBP0FHG
title: "Docs: run the lake from the container image, with a compose example"
type: task
status: done
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
references:
  - ref: pr:forgejo/terva-sh/lampi#91
    path: null
claim: null
archive: null
created_at: 2026-09-28T16:01:57Z
updated_at: 2026-09-28T19:00:44Z
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

- [x] docs/container.md covers quick start, first start, devices, optional features, stopped-lake maintenance, upgrades and logs
- [ ] A checked-in compose example sets the stop grace period, healthcheck, hardening options and secrets
- [x] The guide is linked from docs/README.md, the README and deploy/README.md
- [ ] Every command in the guide was run against a real container

## Implementation plan

Write `docs/container.md` as a how-to, and ship `deploy/compose/` (`compose.yaml`, `Caddyfile`, `env.example`) as the example it walks through. Link the guide from `docs/README.md`, the README, and `deploy/README.md`. Run the stack with `podman-compose`, since this host has no Docker, including an agent that registers through Caddy.

### Decisions

**The operator token goes in through `login` inside the image, not through Compose secrets.** `serve` rewrites a token file with hashes, so a read-only secret mount doesn't fit. A bind-mounted host directory brings ownership trouble for UID 65532. `docker compose run --rm -T lampi login --token-file /var/lib/terva-lampi/tokens/operator.token < operator.token` writes the token into the volume as the lake's user, and it works with the read-only root.

**No `request_body` limit in Caddy.** Caddy streams request bodies and sets no limit, and `serve` refuses a blob over 32 MiB itself. A 41 MiB upload through the proxy reached the lake and was refused there, so a Caddy cap would never take effect first. It would only need changing if the lake's limit grew.

**Response headers.** Caddy sets HSTS, removes `Server`, and adds `X-Content-Type-Options` and `Referrer-Policy` only where the lake didn't set them (the `?` prefix). The dashboard already sends CSP, `nosniff`, `X-Frame-Options: DENY`, and `Referrer-Policy: strict-origin` from `internal/webauth/browser.go`, and those win.

**Certificates.** The guide gives three options:
- a public name, with Let's Encrypt;
- `.home.arpa` or `.internal`, with Caddy's own CA, whose root the agents and the minting lake must trust;
- DNS-01, which needs a Caddy image built with the provider's module.

**Rollback uses an Alpine container,** because the lake image has no shell. The step also deletes `catalog.db-wal` and `catalog.db-shm`. They belong to the newer catalog, and SQLite would apply them to the restored file. The same point went into `docs/vps-bringup.md` and the `serve migrate` help.

## Notes

**agent:claude-code/aa1afd80** at 2026-09-28T18:08:29Z

### The compose example ships a Caddy proxy

The owner asked on 2026-09-28 for the recommended compose setup to include Caddy, or another web server, that terminates TLS and sets the right headers. Make Caddy the default `proxy` service in `deploy/compose/compose.yaml`, with a checked-in `deploy/compose/Caddyfile`. Traefik labels can follow as a variant.

What the Caddyfile does, and why:

- **TLS.** Automatic HTTPS for `lake.example.com`. Document the DNS-01 or internal-CA route for a home server with no public port 80/443, because a tailnet or LAN-only lake can't pass the HTTP challenge.
- **Only the proxy is exposed.** `reverse_proxy lampi:8787` over the compose network. The lake publishes no port, and its default command already has `--behind-proxy`.
- **Body size.** `request_body max_size` sized for chunked uploads: take the value from the go-live 32 MiB drill (TKT-01M3D8YXG) and `docs/vps-bringup.md#tls-in-front`, don't guess it. Check the timeouts for a slow uplink too.
- **Forwarded headers.** Caddy sets `X-Forwarded-For`, `X-Forwarded-Proto`, and `X-Forwarded-Host` by default. `serve` logs X-Forwarded-For as sent and trusts nothing from it. Check whether the OIDC dashboard's `base_url` needs `X-Forwarded-Proto` before relying on it.
- **Response headers.** HSTS; `X-Content-Type-Options: nosniff`; `Referrer-Policy: no-referrer`. For the dashboard, a frame-ancestors or X-Frame-Options deny, unless `internal/web` already sets these. Read what `internal/web` sends first, and don't set a header twice with different values.
- **Paths.** `/.well-known/terva-lampi/` must reach the lake (`serve register` checks it), and so must `/v1/register`. Nothing is rewritten.
- **Metrics.** The Caddyfile doesn't route `/metrics`. Metrics stay on the private network.

Test it with `docker compose up` against a local CA, then register a real agent through it. That end-to-end registration is the part TKT-01M3MC0QV couldn't test.

**agent:claude-code/aa1afd80** at 2026-09-28T18:49:58Z

### What was run

All of this ran with Podman 5 and `podman-compose` 1.6.0 (run through `uv`), using the `just image` build. Compose ran from a copy of `deploy/compose/` with three test-only changes: ports 18080 and 18443, a network alias `lake.home.arpa` on the proxy standing in for DNS, and a `./share` mount.

- **Bootstrap.** `run --rm -T lampi login` wrote the operator token into the volume, and `up -d` brought `lampi` to healthy and then started Caddy, which issued an internal certificate for `lake.home.arpa`.
- **Through the proxy.**
  - `/healthz` returned 200 with HSTS, `nosniff`, and `no-referrer`.
  - `/v1/stats` returned 401 without a token.
  - A 32 MiB authenticated blob upload was stored.
  - A 41 MiB one reached the lake and was refused there, which is why Caddy sets no body limit.
  - The lake container publishes no port.
- **Registration end to end.**
  - `serve identity set-url https://lake.home.arpa` recorded the URL.
  - `serve register --name laptop` (with `SSL_CERT_FILE` set to Caddy's root) fetched the key list through the proxy and minted a code.
  - A separate container on the network ran `register --code-file ... --fingerprint ...`, and `status` reported health ok and catalog counts with the new token.
  - `serve devices` listed both devices.
- **Maintenance.**
  - `stop lampi`, then `run --rm -T lampi serve compact` and `serve migrate --check`, then `start lampi`: the identity was kept.
  - `kill -s HUP lampi` reloaded the tokens and identity.
- **Upgrade and rollback between real images.** This covers the image-level test TKT-01M3MC0RA (Catalog: migrations that are safe for unattended image upgrades) left open.
  - I built an image from f50c57f (schema 11, before `--behind-proxy`), started it, then switched `LAMPI_VERSION` to the current build.
  - `migrate --check` reported 3 pending.
  - `serve` logged the backup, steps 12 to 14, and `11 -> 14`, and kept the identity.
  - The v11 image then refused the catalog.
  - I followed the guide's Alpine steps (list, copy, remove the WAL and SHM files, chown) and started v11 again. It came up on the restored catalog with the same lake id and the operator device.
- **Backup and metrics.** `serve backup --out /backups/lake --token-file ...` into a bind mount, and `--metrics-addr 0.0.0.0:9187 --metrics-public` started its listener.

### Not run

- **Docker itself.** Every `docker compose` command ran as its `podman-compose` equivalent. `docker compose cp` wasn't run; `podman cp` was.
- **The dashboard snippet.** It needs an OIDC provider.
- **Let's Encrypt and DNS-01.** Only Caddy's internal CA was exercised.

### Criteria left unticked

- **Criterion 2** asks for Compose secrets. The token deliberately doesn't use them (see the plan). The healthcheck comes from the image, and Compose waits on it with `depends_on: condition: service_healthy`. Tick it if that is acceptable.
- **Criterion 4** asks for every command to be run against a real container. Everything above ran, but not under Docker, and not the dashboard snippet.

## Summary

Landed in PR #91. docs/container.md walks through deploy/compose/ (compose.yaml, Caddyfile, env.example). The lake is hardened, publishes no port, and waits on its healthcheck before Caddy starts. The operator token goes into the volume through login inside the image. Caddy sets HSTS and fills in nosniff and Referrer-Policy only where the lake didn't, with no body limit (serve refuses blobs over 32 MiB itself). The guide covers certificates, registration, reload, stopped-lake maintenance, backups, upgrade and a rollback that removes the newer catalog's WAL and SHM files, the dashboard and metrics flags, logs, and Podman. Verified under podman-compose, including agent registration through Caddy and a real upgrade and rollback between a schema-11 image and this build. Two criteria are left unticked with the owner's agreement to merge: Compose secrets aren't used, because serve rewrites the token file; and Docker itself plus the dashboard snippet weren't run. The first Docker run comes with TKT-01M3MC0RN (Release CI: publish the multi-arch lake image to GHCR on v* tags).
