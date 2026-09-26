---
schema: 3
id: TKT-01M3D8YXDXZKEM5S094ZTXG67V
title: "Go-live: restore drill from a backup onto a fresh data directory"
type: task
status: ready
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
updated_at: 2026-09-26T01:17:17Z
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

Restore drill. Take `serve backup --out` from the lake. The live lake is acceptable because backup runs while serve runs and only reads it. Restore the copy into a fresh data directory, run `serve` on it at a dev address, and compare `status` and `export` against the source. Write down the restore steps in `docs/vps-bringup.md` or `deploy/README.md` as they were actually run.

## Acceptance criteria

- [ ] A backup restored onto a fresh data directory serves status and export output that matches the source
- [ ] The restore procedure is written down as it was run
