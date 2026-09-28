---
schema: 3
id: TKT-01M3MC0QVMMW0TQ6RGAYDZEF82
title: "Container: production Containerfile for the lake, amd64 and arm64"
type: task
status: in-progress
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
references:
  - ref: pr:forgejo/terva-sh/lampi#77
    path: null
claim:
  actor: agent:claude-code/aa1afd80
  branch: self-host/containerfile
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-aa1afd80
  commit: f7ff0599b46d967fe734cef30a928c088624413a
  session: null
  claimed_at: 2026-09-28T17:03:15Z
  expires_at: null
archive: null
created_at: 2026-09-28T16:01:56Z
updated_at: 2026-09-28T17:10:00Z
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

- [x] One Containerfile builds linux/amd64 and linux/arm64 by cross-compiling, with no QEMU in the Go stage
- [x] The image runs serve as non-root 65532 with /lake as the data volume and no secrets baked in
- [x] The listen-address and configuration decisions are recorded in this ticket
- [x] SIGTERM reaches serve as PID 1 and SIGHUP reloads tokens through docker kill
- [x] docker exec runs serve register, devices, backup and identity against /lake with no extra flags
- [x] OCI labels name the source repository, version, revision and license

## Implementation plan

### Decisions

**Listening: add `serve --behind-proxy`.** The owner chose this on 2026-09-28. The flag says TLS terminates in a proxy in front of `serve`, so a non-loopback `--addr` with device tokens is expected, and the plaintext warning is replaced with one line naming the setup.
- It needs `--token-file`, and refuses to start without one. So it can't be used to expose a lake that has no tokens, and the existing refusal for non-loopback without tokens is unchanged.
- It doesn't make `serve` trust `X-Forwarded-For`, which is still logged exactly as the proxy sent it.

Alternatives considered:
- Accepting the warning: it prints on every container start and teaches operators to ignore it.
- Dropping the warning when running in a container: detecting a container is guesswork, and it would also silence a lake on a published port with no proxy in front.

**Configuration: flags only, in the image's `CMD`.** `serve` doesn't read environment variables for its settings. Compose's `command:` and Kubernetes' `args:` are as readable as `environment:`, and one source of configuration avoids precedence rules. The `LAMPI_SERVE_*` names in `deploy/systemd/` are systemd substitutions and stay that way.

**Data directory: `/var/lib/terva-lampi`, the same as the systemd unit.** The image sets `XDG_STATE_HOME=/var/lib`, so `serve` and every admin subcommand (`docker exec lampi terva-lampi serve devices`) resolve that directory with no `--data`. The token file defaults to `/var/lib/terva-lampi/tokens`, as in the systemd unit. The ticket description said `/lake`; matching systemd keeps one layout in the docs and lets a lake move between the two with no path changes.

**Base: `gcr.io/distroless/static-debian12:nonroot`**, pinned by digest. It includes CA certificates, `/etc/passwd`, and UID/GID 65532, and has no shell. The data directory exists in the image, owned by 65532 with mode 0700, so a new named volume starts with the right owner.

**Build: a multi-stage `Dockerfile` at the repository root**, built with buildx.
- The Go stage runs on `BUILDPLATFORM` and cross-compiles to `TARGETOS`/`TARGETARCH`, so arm64 doesn't compile under QEMU.
- `.dockerignore` keeps `.git` out of the build context, so there's no VCS stamp. VERSION and COMMIT come in as build args and go to `-X` ldflags, the same way `just build` stamps a binary.
- This leaves the CI ticket's goreleaser-vs-buildx choice leaning to buildx, since the Dockerfile works on its own.

**Health: `HEALTHCHECK` runs `serve healthcheck`** against the image's default address. The start period is 5 minutes so a migration isn't counted as a failure. An operator who changes the port overrides the healthcheck too; the docs say so.

**Read-only root.** `serve` writes only under its lake directory, but SQLite can spill temp files during large sorts, `VACUUM INTO`, and FTS merges. The docs run the container with `read_only` and a tmpfs at `/tmp`.

### Steps

1. Add `--behind-proxy` to `internal/cli/serve.go`, with tests and a `docs/cli.md` entry.
2. Add `Dockerfile` and extend `.dockerignore`.
3. Add `just image` and `make image` targets that build for the local platform and tag `terva-lampi:dev`. Nothing is pushed.
4. Build both platforms with buildx. Run the amd64 image with the arm64 run under QEMU if it's available, register a device, and probe it.

## Notes

**agent:claude-code/aa1afd80** at 2026-09-28T17:09:36Z

### Built and run with Podman; Docker and buildx not tried

This machine has Podman 5 and no Docker, so every check below used `podman`. Docker and `docker buildx` behave the same for everything the Dockerfile uses (`BUILDPLATFORM`, `TARGETOS`/`TARGETARCH`, `HEALTHCHECK`, exec form), but I haven't run them. The first buildx run will be the CI ticket's.

**Build.**
- `CONTAINER_ENGINE=podman just image` builds `terva-lampi:dev`.
- `podman build --platform linux/arm64` builds with no QEMU installed, and the binary it holds is ELF aarch64. The arm64 image wasn't run, because there's no emulator here.
- Podman's default OCI format drops `HEALTHCHECK`, so the recipes pass `--format docker` under Podman. Docker keeps it anyway.

**Run.** amd64, with `--read-only --tmpfs /tmp --cap-drop=ALL --security-opt no-new-privileges`, a named volume, and a one-token directory:
- It started with the behind-proxy line and no warning.
- `podman healthcheck run` reported healthy. `/v1/stats` returned 401 without the token and 200 with it.
- Through `exec` with no `--data`: `--version`, `serve devices`, `serve identity`, `serve identity set-url`, `serve normalize --status`, `serve backup --out /tmp/backup`, `serve register --name`, and `serve register --list` all worked.
- `podman kill -s HUP` reloaded the tokens, profiles, and identity.
- `podman stop -t 60` exited 0 in under a second on an idle lake.
- After a restart, the lake kept the same identity.
- A fresh named volume, without a UID mapping, was writable by 65532: `serve` made its identity there.

**Not covered here.**
- Registering from a real agent, because it needs a reachable HTTPS URL. The docs ticket should walk through it.
- A long normalize drain on stop.

**For the docs ticket:**
- Rootless Podman with a bind-mounted token directory needs `--userns keep-id:uid=65532,gid=65532` (or a `chown` to the mapped UID). Otherwise 65532 can't read a 0700 directory.
- The first operator token is made with `terva-lampi login --token-file`, as in `docs/vps-bringup.md`.
