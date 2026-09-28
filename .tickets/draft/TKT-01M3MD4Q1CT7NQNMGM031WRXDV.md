---
schema: 3
id: TKT-01M3MD4Q1CT7NQNMGM031WRXDV
title: "Flaky under load: TestAgentCancelSkipsFailedSyncRetry sees one hello"
type: bug
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/agent
  - area/ci
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-28T16:21:35Z
updated_at: 2026-09-28T16:21:35Z
created_by:
  id: agent:claude-code/2cf53976
  name: ""
updated_by:
  id: agent:claude-code/2cf53976
  name: ""
extensions: {}
---

## Description

`TestAgentCancelSkipsFailedSyncRetry` (`internal/cli/agent_test.go:166`) failed on Forgejo CI run 696, attempt 2, on 2026-09-28, with `hellos 1`. The test expects two hellos: the failed push, then the shutdown drain.

In the failing output, the first sync printed `checked 0, missing 0, …`, so it found nothing to push and sent no hello. The only hello, and the only 503, came from the drain. `waitOut` waits for "503", so it matched the drain's line, and then the counts were off by one.

The test passed 200 of 200 runs with `-race` locally, on `main` at f50c57f and on the PR head, so it fails only under runner load. The code under test in that PR (#66) did not change the agent.

The likely cause is that the first pass runs before the fixture's session file is visible to the agent's scan. Fix: wait for the first sync to report `checked 1` (or for a first hello) before expecting the 503, or have the test's server count hellos only after the first sync has started.

## Acceptance criteria

- [ ] The test waits for the first failed push, not any 503
