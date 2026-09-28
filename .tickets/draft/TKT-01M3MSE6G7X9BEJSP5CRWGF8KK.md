---
schema: 3
id: TKT-01M3MSE6G7X9BEJSP5CRWGF8KK
title: "Flaky in CI: TestAgentRetriesFailedSyncWithoutGrowth times out"
type: bug
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/ci
  - area/agent
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-28T19:56:29Z
updated_at: 2026-09-28T19:56:29Z
created_by:
  id: agent:claude-code/2cf53976
  name: ""
updated_by:
  id: agent:claude-code/2cf53976
  name: ""
extensions: {}
---

## Description

Forgejo CI run 931 (PR #99, which changes only ticket files) failed with agent_test.go:139 'timeout:' after 19.84s; internal/cli took 251s on that runner. waitOut's 15s deadline looks too tight for the runner, as with TKT-01M3MMHMP7.
