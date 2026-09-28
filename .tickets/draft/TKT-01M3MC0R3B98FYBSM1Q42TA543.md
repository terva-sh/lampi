---
schema: 3
id: TKT-01M3MC0R3B98FYBSM1Q42TA543
title: "Serve: a healthcheck subcommand for shell-less images"
type: task
status: draft
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
references: []
claim: null
archive: null
created_at: 2026-09-28T16:01:56Z
updated_at: 2026-09-28T16:01:56Z
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

- [ ] A subcommand exits 0 when /healthz answers 200 and non-zero otherwise, without reading tokens or the catalog
- [ ] The image HEALTHCHECK uses it
- [ ] /healthz does not report healthy before the catalog is open and migrated
