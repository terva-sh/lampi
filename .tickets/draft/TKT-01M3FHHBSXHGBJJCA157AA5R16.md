---
schema: 3
id: TKT-01M3FHHBSXHGBJJCA157AA5R16
title: "Onboarding: end-to-end validation of both paths and operator docs"
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/ops
  - area/docs
assignees: []
milestone: null
parent: TKT-01M3FHHBCJ12FXKNTB6Z138F6N
origin: null
dependencies:
  - TKT-01M3FHHBREE42QFPAN0YFHSH98
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-26T19:02:12Z
updated_at: 2026-09-26T19:02:12Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/e4a47e8c
  name: ""
extensions: {}
---

## Description

Part of the agent onboarding epic. Prove both onboarding paths end to end, then rewrite the operator docs around them.

### Validation

Use real `serve` subprocesses, as `goLiveServe` in `internal/cli/golive_test.go` does, and isolated XDG directories. No production host and no real credentials.

- Path 1: mint a code on lake A, register a fresh client from it, and sync. The device appears by name, the base configuration applies, and a second sync posts nothing.
- Path 2: start an agent with no lake and confirm that it uploads nothing. Register lake A and then lake B without restarting it. Sessions allowed for A only reach A, sessions allowed for both reach both, and a local deny stops both.
- Failure cases: a used code, an expired code, a tampered code, the wrong key at the code's URL, a revoked device, and lake B down while A keeps syncing.
- Upgrade: a legacy single-lake client state and a legacy token file on the lake both upgrade in place, and the next sync uploads nothing.

### Docs

Rewrite `docs/vps-bringup.md` "Device token" and "Check, then point the agents", the README quickstart, and `deploy/README.md` around `serve register` and `terva-lampi register`. Keep the manual token-file path documented as the fallback.

## Acceptance criteria

- [ ] Real-subprocess tests cover path 1, path 2 with two lakes, the failure cases and the legacy upgrade
- [ ] The vps-bringup, README and deploy docs lead with registration and keep the manual token path as a fallback
