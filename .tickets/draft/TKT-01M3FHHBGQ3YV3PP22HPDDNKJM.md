---
schema: 3
id: TKT-01M3FHHBGQ3YV3PP22HPDDNKJM
title: "Named devices: device ids on tokens, attribution, list and revoke"
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/auth
  - area/server
  - area/catalog
assignees: []
milestone: null
parent: TKT-01M3FHHBCJ12FXKNTB6Z138F6N
origin: null
dependencies:
  - TKT-01M3FHHBDS7VCKK5AJ7T3H6DYX
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-26T19:02:11Z
updated_at: 2026-09-26T19:02:11Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/e4a47e8c
  name: ""
extensions: {}
---

## Description

Part of the agent onboarding epic. Give each device token a name and an id so that a registered device can be listed, attributed and revoked.

Today `internal/auth/devices.go` keeps only `[][32]byte` hashes. `Match` returns a bool, and `authed` in `internal/api/server.go` adds no identity to the request. Attribution comes from the `machine_id` the client asserts in its manifest, and nothing binds that id to a token.

- Store devices in the lake (in the catalog, or in a file beside it): id, name, token hash, created time, registration source (code or legacy file), revoked time.
- Keep `--token-file` working. Its entries load as legacy devices named after the file or the `#` comment, so the hosted lake upgrades without re-issuing tokens.
- `authed` puts the device id in the request context. The access log and the manifest handler record it. Refuse a manifest whose `machine_id` is already bound to a different device, and log it.
- `serve devices list` and `serve devices revoke NAME` work against a running lake the way SIGHUP reload does today, with no restart and no requests dropped.

## Acceptance criteria

- [ ] Every authenticated request carries a device id, and the access log records it
- [ ] Legacy --token-file entries load as named devices with no re-issue
- [ ] serve devices list and revoke work on a running lake without dropping requests
- [ ] A machine_id bound to one device is refused from another
