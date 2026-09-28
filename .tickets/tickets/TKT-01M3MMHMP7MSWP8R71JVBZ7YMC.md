---
schema: 3
id: TKT-01M3MMHMP7MSWP8R71JVBZ7YMC
title: Profile push test bounds the edit at 5s, which a slow runner exceeds
type: bug
status: in-progress
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
claim:
  actor: agent:claude-code/2cf53976
  branch: tests/profile-push-bound
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-2cf53976
  commit: 8fe357d3cfbda440f0dc6aaac8b6f056cbd6f888
  session: null
  claimed_at: 2026-09-28T18:30:59Z
  expires_at: null
archive: null
created_at: 2026-09-28T18:30:59Z
updated_at: 2026-09-28T18:31:15Z
created_by:
  id: agent:claude-code/2cf53976
  name: ""
updated_by:
  id: agent:claude-code/2cf53976
  name: ""
extensions: {}
---

## Description

TestAgentFetchesAProfileEditWithinSeconds failed in CI run 836 (PR #87) with 'the edit took 6.49s to reach the agent'. The runner is about 13x slower than a workstation; internal/cli took 213s. The nudge is not held by profileNudgeGap: the first nudge fires at once. The time is the report, fetch, verify, reload and sync on a slow host.

## Notes

**agent:claude-code/2cf53976** at 2026-09-28T18:31:15Z

Dropped the 5s wall-clock check and kept waitOut's 15s deadline, which is far inside the agent's hourly fetch, so the test still proves that only the version header can bring the edit. Alternatives: raising the bound to 10s (still measures the runner, and 15s is waitOut's own limit anyway); shortening profileNudgeGap in the test (the gap was not the delay, since the first nudge fires at once).
