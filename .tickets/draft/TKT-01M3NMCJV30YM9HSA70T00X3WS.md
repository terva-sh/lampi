---
schema: 3
id: TKT-01M3NMCJV30YM9HSA70T00X3WS
title: "Flaky under load: TestAgentReportsItsSyncAndProfileToTheLake"
type: bug
status: draft
status_reason: null
priority: low
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
created_at: 2026-09-29T03:47:27Z
updated_at: 2026-09-29T03:47:27Z
created_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
extensions: {}
---

## Description

Failed once in a full local `just ci` on 2026-09-28 (branch t3code/add-raw-session-option, admin role work, which touches no agent code) with "no report of the upload" at internal/cli/agent_report_test.go:54, after a 10 s wait, and a TempDir cleanup error. It passed 5 times alone (-count=5) and in a full `go test ./internal/cli` right after. It looks like a timing window under parallel load, like TKT-01M3MSE6 (Flaky in CI: TestAgentRetriesFailedSyncWithoutGrowth times out).
