---
schema: 3
id: TKT-01M3MJDSWVHC8Z5JVZCMZS5XP1
title: "Flaky under load: TestAgentReportsItsSyncAndProfileToTheLake"
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
created_at: 2026-09-28T17:53:56Z
updated_at: 2026-09-28T17:53:56Z
created_by:
  id: agent:claude-code/aa1afd80
  name: ""
updated_by:
  id: agent:claude-code/aa1afd80
  name: ""
extensions: {}
---

## Description

`TestAgentReportsItsSyncAndProfileToTheLake` (`internal/cli/agent_report_test.go`) failed once in a local `just ci` run on 2026-09-28, on branch `self-host/migrations`. That branch doesn't touch the agent or its reports.

After 10.01s it failed with `no report of the upload`. The only report the lake held had `LastSync` set, but it didn't show the upload yet, although the agent's log already said `uploaded 1, manifests 1`. The agent had also restarted its work once after a profile reload (`reload: restarted work`).

Run alone with `-count=8`, the test passed every time, and the next full `just ci` passed. So it's timing: under full-suite load, the report that carries the upload doesn't arrive within the test's 10-second wait. The reload restart may delay or drop the report sent after the first pass.

Look at when the agent sends a report after a pass that follows a restart, and whether the test waits for the right one.

## Acceptance criteria

- [ ] The test passes under a loaded full-suite run, or its wait is tied to the report it expects
