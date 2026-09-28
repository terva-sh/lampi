---
schema: 3
id: TKT-01M3JV4618D6NRDY9WEZ1VS7P4
title: "Serve: Prometheus text metrics on an opt-in listener"
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/ops
  - area/server
assignees: []
milestone: null
parent: TKT-01M3JV3TRWB6JQQGY1F84JCFDE
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-28T01:47:29Z
updated_at: 2026-09-28T01:47:35Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/e4a47e8c
  name: ""
extensions: {}
---

## Description

Expose the lake's operating numbers in the Prometheus text format, so an
existing monitoring stack can scrape them and alert, for example when the
disk is nearly full or a device stops syncing.

The endpoint listens on its own address, set with `--metrics-addr`, and
is off by default. It refuses a non-loopback address unless
`--metrics-public` is given, because it has no authentication.

It exports:

- storage gauges from the latest sample
- queue depths
- per-device seconds since the last accepted update
- request counters by route class and status
- bytes accepted
- build info

It is written by hand in the text format, with no new dependency.

## Acceptance criteria

- [ ] --metrics-addr serves /metrics; off by default; non-loopback needs --metrics-public
- [ ] Storage, queue, device, request and build metrics are exported
