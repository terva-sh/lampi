---
schema: 3
id: TKT-01M3MC0QVMMW0TQ6RGAYDZEF82
title: "Container: production Containerfile for the lake, amd64 and arm64"
type: task
status: ready
status_reason: null
priority: high
due_on: null
labels:
  - area/ops
  - area/server
assignees: []
milestone: null
parent: TKT-01M3MC023P4A5H7PTF662QSSM8
origin: null
dependencies:
  - TKT-01M3MC0R3B98FYBSM1Q42TA543
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-28T16:01:56Z
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

Build a production image of `terva-lampi` that runs the lake, for `linux/amd64` and `linux/arm64`. This is a different image from `e2e/Dockerfile`, which stays a test fixture: it is not meant to be exposed and is never pushed.

### What the image needs

- **Base.** The binary is static (`CGO_ENABLED=0`, pure-Go SQLite), so a distroless or scratch base works. Start from `gcr.io/distroless/static-debian12:nonroot`: it includes CA certificates, `/etc/passwd`, and UID/GID 65532, and has no shell. If there's a reason to pick another base, record it here.
- **Cross-compilation, not emulation.** Use buildx with `--platform` and cross-compile in the Go stage with `TARGETOS`/`TARGETARCH` on `BUILDPLATFORM`, so an arm64 build doesn't run under QEMU.
- **User and data.** The image runs as non-root 65532. Declare `/lake` as the data volume and document that its ownership must match. Keep secrets out of the image: no tokens, config, or `identity.json`.
- **Listening.** Inside the container, `serve` has to bind `0.0.0.0:8787` so a published port or a proxy container can reach it. Today that needs `--token-file` and prints a warning on every start, which is noise in a container that is only on a private network behind a TLS proxy. Decide between:
  - accepting the warning;
  - an explicit flag such as `--behind-proxy` that acknowledges the setup and keeps the refusal when there is no token file;
  - removing the warning when running in a container.

  Record the choice and the reasons in this ticket. Don't weaken the "no tokens, loopback only" rule.
- **Configuration.** Compose and Kubernetes users expect environment variables. Either teach `serve` to read the `LAMPI_SERVE_*` names already used in `deploy/systemd/serve.env.example` (with a flag overriding its variable), or keep flags only and put them in the documented `command:`. Secrets stay file paths either way (`--token-file`, the OIDC `client_secret_file`) so they can be container secrets.
- **Stopping.** `serve` needs up to about 50s to drain. The image can't set the stop timeout, so the docs and the compose example have to (see the docs ticket). Check that PID 1 gets SIGTERM directly (exec form `ENTRYPOINT`, no shell wrapper) and that SIGHUP still reloads tokens and profiles through `docker kill -s HUP`.
- **Health.** Wire the `HEALTHCHECK` to the probe from the sibling ticket once it exists.
- **Metadata.** OCI labels: `org.opencontainers.image.source` pointing at `https://github.com/terva-sh/lampi` (this links the GHCR package to the repository), plus version, revision, created, and licenses (MIT). `--version` in the image reports the release tag.
- **Admin commands.** `docker exec <c> terva-lampi serve register|devices|backup|identity ...` works against `/lake` without extra flags. Point the image's default `--data` at `/lake`, or set it through the environment.

### Out of scope

Publishing (see the registry and CI tickets) and a compose file (see the docs ticket).

## Acceptance criteria

- [ ] One Containerfile builds linux/amd64 and linux/arm64 by cross-compiling, with no QEMU in the Go stage
- [ ] The image runs serve as non-root 65532 with /lake as the data volume and no secrets baked in
- [ ] The listen-address and configuration decisions are recorded in this ticket
- [ ] SIGTERM reaches serve as PID 1 and SIGHUP reloads tokens through docker kill
- [ ] docker exec runs serve register, devices, backup and identity against /lake with no extra flags
- [ ] OCI labels name the source repository, version, revision and license
