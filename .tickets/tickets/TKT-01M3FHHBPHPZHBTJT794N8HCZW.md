---
schema: 3
id: TKT-01M3FHHBPHPZHBTJT794N8HCZW
title: "Agent: fan out to many lakes with per-lake outbox, backoff, status"
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/agent
assignees: []
milestone: null
parent: TKT-01M3FHHBCJ12FXKNTB6Z138F6N
origin: null
dependencies:
  - TKT-01M3FHHBN7G90MR0BJ89DGPRQQ
  - TKT-01M3FP1107KXYARCAYVYT2Y409
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-26T19:02:11Z
updated_at: 2026-09-26T20:20:46Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/e4a47e8c
  name: ""
extensions: {}
---

## Description

Part of the agent onboarding epic. Run one agent against several lakes.

One process, one watch, one scan per pass. Each pass routes each session through each lake's allowlist and pushes to each lake independently. The outbox, backoff, 401/403 logging and debounce ceiling are per lake, so a lake that is down or refusing the token does not hold back another. Today these are a single `bo` and `authLogged` in `runAgentLoop` (`internal/cli/agent.go`).

`status` prints a block per lake: URL and source, lake id, health, stats, outbox depth, last sync and last error. `sync` pushes to every lake unless `--lake` is given.

## Acceptance criteria

- [ ] One agent pushes to each lake by that lake's allowlist, with outbox, backoff and auth logging kept per lake
- [ ] A lake that is down or refusing the token does not delay uploads to another lake
- [ ] status prints one block per lake, and sync pushes to every lake unless --lake is given
