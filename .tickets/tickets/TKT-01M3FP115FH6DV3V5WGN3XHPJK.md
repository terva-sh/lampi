---
schema: 3
id: TKT-01M3FP115FH6DV3V5WGN3XHPJK
title: "Agent: standalone mode with no lake, and lake reload on SIGHUP"
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
  - TKT-01M3FHHBPHPZHBTJT794N8HCZW
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-26T20:20:39Z
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

Part of the agent onboarding epic. Let the agent run with no lake, and change its set of lakes without a restart.

- With no lake configured, the agent starts, discovers and watches, uploads nothing, and says so once. This is the standalone mode in the epic. Today an empty config falls back to the loopback default, so the no-lake state must be explicit: a legacy config with no `server` keeps that fallback, and a config with an empty `lakes` map and no `server` means no lake.
- On Unix the agent reloads its lake set on SIGHUP, without dropping in-flight work. `register` and `lakes remove` send the signal through `agent.pid`, the way the hook sends SIGUSR1. Other config still reloads on restart only.
- On Windows there is no reload. The owner decided on 2026-09-27 that a restart is required there, and the commands say so.

## Acceptance criteria

- [ ] With no lake configured, the agent watches, uploads nothing and says so once
- [ ] On Unix, SIGHUP reloads the lake set without dropping in-flight work
- [ ] On Windows the commands that change lakes say a restart is required
