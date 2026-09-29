---
schema: 3
id: TKT-01M3MJDSWVHC8Z5JVZCMZS5XP1
title: "Flaky under load: TestAgentReportsItsSyncAndProfileToTheLake"
type: bug
status: in-progress
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
claim:
  actor: agent:claude-code/cd41c9ac
  branch: fix/agent-report-flake
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-fdd1a9d1
  commit: 913b2b4acfb6bc3882385ca7099dc9b8e898fd5f
  session: null
  claimed_at: 2026-09-29T05:05:00Z
  expires_at: null
archive: null
created_at: 2026-09-28T17:53:56Z
updated_at: 2026-09-29T05:08:51Z
created_by:
  id: agent:claude-code/aa1afd80
  name: ""
updated_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
extensions: {}
---

## Description

`TestAgentReportsItsSyncAndProfileToTheLake` (`internal/cli/agent_report_test.go`) failed once in a local `just ci` run on 2026-09-28, on branch `self-host/migrations`. That branch doesn't touch the agent or its reports.

After 10.01s it failed with `no report of the upload`. The only report the lake held had `LastSync` set, but it didn't show the upload yet, although the agent's log already said `uploaded 1, manifests 1`. The agent had also restarted its work once after a profile reload (`reload: restarted work`).

Run alone with `-count=8`, the test passed every time, and the next full `just ci` passed. So it's timing: under full-suite load, the report that carries the upload doesn't arrive within the test's 10-second wait. The reload restart may delay or drop the report sent after the first pass.

Look at when the agent sends a report after a pass that follows a restart, and whether the test waits for the right one.

## Acceptance criteria

- [x] The test passes under a loaded full-suite run, or its wait is tied to the report it expects

## Implementation plan

Tie the test's wait to what the lake is designed to keep: the session ingested and a report whose last sync saw it (uploaded or unchanged), instead of the newest report showing the upload. No agent change.

## Notes

**agent:claude-code/2cf53976** at 2026-09-28T19:54:29Z

Seen again 2026-09-28 in a local just ci (load average 17 from other sessions' runs): failed once at 10.02s, then passed 10/10 and the whole package passed once load dropped.

**agent:claude-code/cd41c9ac** at 2026-09-29T05:08:51Z

### Cause: the test waited for a report the lake may overwrite

The lake keeps only a device's newest report, and a report carries only
the newest sync (`syncOutcome.last` in `internal/cli/agent_report.go`).
The test waited for that newest report to show `Uploaded == 1`.

Agent logs compared:

- **A passing run** makes one pass (`uploaded 1`). The `unchanged 1`
  line after it is the shutdown drain, which runs after the test has
  checked.
- **Both failing runs** (2026-09-28, on `self-host/migrations` and on
  `t3code/add-raw-session-option`) logged the profile reload *before*
  `watching`. Right after the upload came a second real pass
  (`checked 0 … unchanged 1`, with no `drain:` prefix). Its report
  replaced the upload's before the 20 ms poll saw the upload.

Under load the profile answer can land before the watch starts. The
reload then leaves one extra pass. That pass is harmless agent behavior:
a sync that finds the session already in the lake.

### Fix: the test waits for the report it actually expects

The test now waits for two things together:

- the session in the lake (`Counts().Sessions == 1`)
- a report whose last sync saw it (`Uploaded + Unchanged == 1`)

The agent code is unchanged.

### Evidence

- **Simulated overwrite.** `noteSync` was temporarily patched to turn an
  upload into an `unchanged 1` outcome, keeping the inventory. This is
  what the extra pass does, made certain. The old test failed with the
  field message `no report of the upload`. The new test passed 5 of 5.
- **Normal runs.** The new test passed 10 of 10 with `-count=10`.
- **Unconfirmed.** I couldn't force the real reload-before-watch timing
  under load: 15 runs with `-race`, `GOMAXPROCS=2` and six busy loops
  all passed. So the cause of the *extra pass* comes from the two
  failure logs, not a reproduction. The fix doesn't depend on it: any
  later unchanged pass is now accepted.
