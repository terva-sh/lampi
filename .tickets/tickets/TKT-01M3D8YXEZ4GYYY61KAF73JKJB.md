---
schema: 3
id: TKT-01M3D8YXEZ4GYYY61KAF73JKJB
title: "Go-live: kill mid-sync, fsck clean, next sync converges"
type: task
status: blocked
status_reason: Process-kill check passed. A disposable VM and its documented hard-power-off mechanism are needed for the remaining power-loss criterion; the shared host must not be killed.
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
updated_at: 2026-09-26T16:38:57Z
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

- [x] After a SIGKILL of serve mid-sync on a dev lake, fsck is clean and the next sync converges
- [ ] The same holds after a hard kill of the VPS VM

## Implementation plan

Run a real isolated serve subprocess and sync a seeded batch through an observing loopback proxy. After several manifest commits, kill serve with SIGKILL while the sync still has pending work. Require sync failure, restart the same lake, run fsck without repair, retry with the same agent state and verify complete counts and an unchanged final sync. Record this only against the process-kill criterion; do not hard-kill the shared deployment host as a substitute for a disposable VM test.

## Notes

**agent:codex/rollout** at 2026-09-26T16:38:57Z

TestGoLiveProcessCrash passed in 3.07s: SIGKILL after 10 manifest commits during a 500-session sync, restart, clean fsck without repair, retry to 500 sessions/artifacts, final unchanged sync and clean fsck. The full VM-power-loss criterion remains untested. The deployment host also runs unrelated user services; a hard kill of that shared host is not an acceptable substitute for a disposable VM drill.

**agent:codex/rollout** at 2026-09-26T16:38:57Z

in-progress to blocked: Process-kill check passed. A disposable VM and its documented hard-power-off mechanism are needed for the remaining power-loss criterion; the shared host must not be killed.
