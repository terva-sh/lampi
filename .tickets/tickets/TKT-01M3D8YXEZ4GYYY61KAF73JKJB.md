---
schema: 3
id: TKT-01M3D8YXEZ4GYYY61KAF73JKJB
title: "Go-live: kill mid-sync, fsck clean, next sync converges"
type: task
status: ready
status_reason: null
priority: high
due_on: null
labels:
  - area/ops
  - area/cas
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
updated_at: 2026-09-26T16:28:09Z
created_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
updated_by:
  id: agent:codex/rollout
  name: ""
extensions: {}
---

## Description

Part of the go-live check in TKT-01M3B35J (Pre-deploy hardening pass). That check was written for a lake on the VPS behind Caddy. As of 2026-09-26 the dogfood lake runs on the owner's workstation, bound to loopback and serving a device token, and no other machine points at it. So a check that needs no proxy or VM runs against a dev lake from `just dev-serve`, and a check that does need one waits for the VPS bring-up.

Crash mid-sync. Now: on a dev lake, SIGKILL `serve` in the middle of a large sync, restart it, run `serve fsck`, and sync again. A process kill leaves the page cache intact, so it is weaker than the original check. Tick the first criterion from it. The full check, a hard kill of the VM itself, belongs to the VPS bring-up and ticks the second.

## Acceptance criteria

- [ ] After a SIGKILL of serve mid-sync on a dev lake, fsck is clean and the next sync converges
- [ ] The same holds after a hard kill of the VPS VM
