---
schema: 3
id: TKT-01M3FHHBPHPZHBTJT794N8HCZW
title: "Agent: fan out to many lakes, run with none, reload lakes on SIGHUP"
type: task
status: draft
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

Part of the agent onboarding epic. Run one agent against several lakes, and let it run with none.

- One process, one watch, one scan per pass. Each pass routes each session through each lake's allowlist and pushes to each lake independently. The outbox, backoff, 401/403 logging and debounce ceiling are per lake, so one lake that is down or refusing the token does not hold back another.
- With no lake configured, the agent starts, discovers and watches, uploads nothing, and says so once. This is the standalone mode in the epic.
- The agent reloads its lake set without a restart when `terva-lampi register` or `terva-lampi lakes remove` changes it. It reloads on SIGHUP, and the CLI sends that signal through `agent.pid` the way the hook sends SIGUSR1. Other config still reloads on restart only.
- `status` prints a block per lake: URL and source, lake id, health, stats, outbox depth, last sync and last error.

## Acceptance criteria

- [ ] One agent pushes to each lake by that lake's allowlist, with outbox, backoff and auth logging kept per lake
- [ ] A lake that is down or refusing the token does not delay uploads to another lake
- [ ] With no lake configured, the agent watches, uploads nothing and says so once
- [ ] SIGHUP reloads the lake set without dropping in-flight work, and status prints one block per lake
