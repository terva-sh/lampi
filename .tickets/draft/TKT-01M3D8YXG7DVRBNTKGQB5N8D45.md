---
schema: 3
id: TKT-01M3D8YXG7DVRBNTKGQB5N8D45
title: "Go-live: 32 MiB session through the TLS proxy on a 2 Mbit uplink"
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area/ops
  - area/server
assignees: []
milestone: null
parent: TKT-01M3B35JS8J2F83FG4J7ZC0199
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-25T21:53:50Z
updated_at: 2026-09-25T21:53:50Z
created_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
extensions: {}
---

## Description

Part of the go-live check in TKT-01M3B35J (Pre-deploy hardening pass). That check was written for a lake on the VPS behind Caddy. As of 2026-09-26 the dogfood lake runs on the owner's workstation, bound to loopback and serving a device token, and no other machine points at it. So a check that needs no proxy or VM runs against a dev lake from `just dev-serve`, and a check that does need one waits for the VPS bring-up.

Throttled uplink. The synthetic container (`e2e/`) uploads a 32 MiB session through the TLS proxy on an uplink limited with, for example, `tc qdisc ... rate 2mbit`. The upload must complete within the proxy and serve deadlines. There is no proxy while the lake is on loopback, so this waits for the VPS bring-up or for a proxy placed in front of the network lake.

## Acceptance criteria

- [ ] A 32 MiB session uploads through the proxy on a 2 Mbit uplink without a timeout
