---
schema: 3
id: TKT-01M3D8YXBNVR3D95XFQ1RXMM3G
title: "Go-live: 20k seeded sessions, second unchanged sync is fast and silent"
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area/ops
  - area/agent
assignees: []
milestone: null
parent: TKT-01M3B35JS8J2F83FG4J7ZC0199
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-25T21:53:49Z
updated_at: 2026-09-25T21:53:49Z
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

The 20k-session case. Seed about 20,000 sessions into the dev harness homes. `testharness.PlantTerva`, `PlantClaude`, `PlantCodex` and `PlantOpenCode` can do it, but they are a Go test API with no command, so the seeding needs a small driver or a test behind a build tag. Allowlist the seeded cwd in `.dev/config`. Run `just dev sync` twice. The first run must succeed. The second must finish in seconds and post no manifest. Record both timings and the catalog counts.

## Acceptance criteria

- [ ] The first sync of about 20k seeded sessions succeeds against a dev lake
- [ ] A second unchanged sync finishes in seconds and posts no manifest, with the timings recorded
