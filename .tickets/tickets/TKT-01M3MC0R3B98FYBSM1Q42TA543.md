---
schema: 3
id: TKT-01M3MC0R3B98FYBSM1Q42TA543
title: "Serve: a healthcheck subcommand for shell-less images"
type: task
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
  - area/ops
assignees: []
milestone: null
parent: TKT-01M3MC023P4A5H7PTF662QSSM8
origin: null
dependencies: []
blocks_on: none
references:
  - ref: pr:forgejo/terva-sh/lampi#74
    path: null
claim:
  actor: agent:claude-code/aa1afd80
  branch: t3code/review-lampi-lake-containerization
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-aa1afd80
  commit: 8242424255a21e030b5e52e9f418a837b44c94b1
  session: null
  claimed_at: 2026-09-28T16:03:34Z
  expires_at: null
archive: null
created_at: 2026-09-28T16:01:56Z
updated_at: 2026-09-28T16:06:48Z
created_by:
  id: agent:claude-code/aa1afd80
  name: ""
updated_by:
  id: agent:claude-code/aa1afd80
  name: ""
extensions: {}
---

## Description

The production image is distroless and has no shell, `curl`, or `wget`, so a Docker `HEALTHCHECK`, a Compose `healthcheck:`, or an exec probe can't call `GET /healthz` the usual way. Kubernetes can use an `httpGet` probe, but Docker and Podman can't.

Add a subcommand that probes a running lake and exits 0 or non-zero, for example `terva-lampi serve healthcheck [--addr 127.0.0.1:8787] [--timeout 3s]`:

- It calls `GET /healthz` on the given address, which defaults to the `serve` default. No token, because `/healthz` is open.
- It prints nothing on success and one line on failure. It exits quickly, with no retries of its own, because the orchestrator retries.
- It never reads the device token, config, or catalog.

Check what `/healthz` reports during start-up. The catalog opens and migrates before the listener binds, so a long migration currently looks like "not up yet" rather than "unhealthy". That's the behavior we want, provided the healthcheck `start_period` is documented. If `/healthz` can answer before the catalog is ready, fix it or add a separate readiness state.

`/healthz` requests aren't logged when they return 200, so a probe every few seconds doesn't fill the logs. Keep it that way.

## Acceptance criteria

- [x] A subcommand exits 0 when /healthz answers 200 and non-zero otherwise, without reading tokens or the catalog
- [ ] The image HEALTHCHECK uses it
- [x] /healthz does not report healthy before the catalog is open and migrated

## Implementation plan

Add `terva-lampi serve healthcheck [--addr 127.0.0.1:8787] [--timeout 3s]` in `internal/cli/healthcheck.go`, dispatched from `runServe` next to the other serve subcommands.

- It sends `GET http://ADDR/healthz`, the way `status` probes a lake (`probeHealth`), and succeeds only on a 200 whose body is `{"status":"ok"}`. Any other result is an error, which `main` prints as one `terva-lampi: ...` line before exiting 1. Success prints nothing.
- `--addr` takes the same form as serve's `--addr`, so a container can pass the value it gave `serve`. An unspecified host (`0.0.0.0`, `[::]`, or an empty host as in `:8787`) is dialled on loopback, because that is where a probe inside the container reaches a server bound to every interface.
- The transport has no proxy. `HTTP_PROXY` in a container's environment must not send a local probe off the host. No redirects are followed and no retries are made: the orchestrator retries.
- It reads no token, config, or lake directory.

The listener binds only after `api.Open` has migrated the catalog (`runServe` in `internal/cli/serve.go`), so `/healthz` can't answer before the catalog is ready. A comment at the `net.Listen` call keeps that order. No test pins it: nothing in the suite can hold a migration open long enough to probe during it.

Tests: success against an `httptest` server, failure on a non-200 and on a 200 with another body, failure on a redirect, failure on a refused connection within the timeout, unspecified-host mapping, and bad arguments.

## Notes

**agent:claude-code/aa1afd80** at 2026-09-28T16:06:28Z

### Implemented; the image HEALTHCHECK criterion waits on the Containerfile

`serve healthcheck` is in `internal/cli/healthcheck.go`, with its tests in `healthcheck_test.go` and a row in `docs/cli.md`. `GOFLAGS=-mod=mod just ci` passes.

I ran the built binary against a scratch lake on 127.0.0.1:18799, away from the live lake on :8787:

- with no server, exit 1 and one line naming the refused connection;
- with `serve` up, exit 0 using `--addr 0.0.0.0:18799`, the form a container passes;
- after SIGTERM, exit 1;
- on a catalog set to `user_version = 999`, `serve` refused to start and the probe never passed.

**Criterion 2 is unticked.** "The image HEALTHCHECK uses it" needs the production image, which doesn't exist yet. TKT-01M3MC0QV ("Container: production Containerfile for the lake, amd64 and arm64") depends on this ticket and already says to wire it in, so tick it when that lands.

**Dropped a test.** I wrote a test that `HTTP_PROXY` is ignored, then removed it: Go's default transport already bypasses the proxy for loopback, so the test passed either way and proved nothing. The transport still sets `Proxy: nil`, which matters when `--addr` names a non-loopback host.
