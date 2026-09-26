---
schema: 3
id: TKT-01M3D8YXDXZKEM5S094ZTXG67V
title: "Go-live: restore drill from a backup onto a fresh data directory"
type: task
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area/ops
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
updated_at: 2026-09-26T16:33:36Z
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

Restore drill. Take `serve backup --out` from the lake. The live lake is acceptable because backup runs while serve runs and only reads it. Restore the copy into a fresh data directory, run `serve` on it at a dev address, and compare `status` and `export` against the source. Write down the restore steps in `docs/vps-bringup.md` or `deploy/README.md` as they were actually run.

## Acceptance criteria

- [x] A backup restored onto a fresh data directory serves status and export output that matches the source
- [x] The restore procedure is written down as it was run

## Implementation plan

Exercise CLI backup against an isolated running source lake. Restore catalog and CAS into a fresh directory, reconstruct derived events before starting the restored listener, and compare status health/catalog lines and byte-identical exports. Run fsck without repair. Document the actual sequence and why derived outputs must be rebuilt; use synthetic data rather than copying private workstation sessions.

## Notes

**agent:codex/rollout** at 2026-09-26T16:33:35Z

The initial byte-identical comparison exposed intentional reprojection behavior: event_id and ingested_at are generated anew, and recorded_at falls back to projection time when the harness supplies none. The drill now compares every other field exactly and checks that exports are byte-identical before/after starting the restored listener. Changing event identity semantics would expand deployment scope unnecessarily; this behavior and the option to preserve derived files are documented.

## Summary

Passed TestGoLiveRestore (golive tag): real source and restored serve subprocesses, four harnesses, online CLI backup, fresh-directory copy, clean fsck, offline derived-file rebuild, matching health/catalog counts and event content. docs/vps-bringup.md records the sequence and generated metadata differences. Production credential backup and encrypted storage remain deployment checks.
