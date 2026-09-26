---
schema: 3
id: TKT-01M3FHHBGQ3YV3PP22HPDDNKJM
title: "Named devices: device ids on tokens, attribution, list and revoke"
type: task
status: in-progress
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
claim:
  actor: agent:claude-code/e4a47e8c
  branch: onboarding/named-devices
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-e4a47e8c
  commit: 737ae01a2696fc2a9835ea1a9ce4f77c0b7d7638
  session: null
  claimed_at: 2026-09-26T21:02:04Z
  expires_at: null
archive: null
created_at: 2026-09-26T19:02:11Z
updated_at: 2026-09-26T21:02:04Z
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
- **Legacy binding, decided by the owner on 2026-09-27:** a legacy token binds to the first `machine_id` it uploads under after the upgrade. `serve devices unbind NAME` clears the binding so the next upload binds again. A legacy token already shared by two machines is refused on the second and logged, naming the device.
- Device creation, binding, unbinding and revocation append to the lake's audit log (a JSONL file in the data directory, covered by backup).
- `serve devices list` and `serve devices revoke NAME` work against a running lake the way SIGHUP reload does today, with no restart and no requests dropped.

The device store bumps the catalog schema version with a migration, if it lives in the catalog.

## Acceptance criteria

- [x] Every authenticated request carries a device id, and the access log records it
- [x] Legacy --token-file entries load as named devices with no re-issue
- [x] serve devices list and revoke work on a running lake without dropping requests
- [x] A machine_id bound to one device is refused from another

## Implementation plan

Catalog schema 5 adds devices (id, unique name, unique token_sha256, source, profile, machine_id with a partial unique index, created/detached/revoked). auth.Devices keeps a name per hash from <name>.token or the # comment above a token. api.SyncDevices records the token file as devices at serve start and after each SIGHUP reload, marking tokens that left the file detached. authed looks the token's hash up in the catalog on each request: a revoked device is 401 at once, the device name and id go to the access log and the request context. The manifest handler binds the device to its first machine_id and refuses another machine or a machine another device holds with 403. serve devices list|revoke|unbind run beside serve. Events go to audit.jsonl (new internal/audit), which backup copies.

## Notes

**agent:claude-code/e4a47e8c** at 2026-09-26T21:02:04Z

Choices: revocation is a per-request catalog read, not a signal, so the operator command works on a running lake without knowing its pid; the cost is one indexed read per request. Revoke is final and a token returning to the file does not undo it, so a revoked laptop cannot come back by an editor undo. A token enrolled with Allow and no catalog row (tests only) authenticates with no device and binds nothing; production always syncs first. The audit log is JSONL with one O_APPEND write per event so serve and the CLI can both append. Pending codes and the registration source land with TKT-01M3FHHBJ (Registration codes); the profile column is there for TKT-01M3FHHBK. Evidence: internal/api/devices_test.go, internal/catalog/devices_test.go, internal/auth/devices_test.go, internal/cli/devices_test.go; go test -race ./... green.
