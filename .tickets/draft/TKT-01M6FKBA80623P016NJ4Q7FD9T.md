---
schema: 3
id: TKT-01M6FKBA80623P016NJ4Q7FD9T
title: "sqlitesnap: TestTakeIsConsistentUnderAWriter flakes under load"
type: bug
status: draft
status_reason: null
priority: normal
due_on: null
labels:
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
created_at: 2026-11-03T02:20:56Z
updated_at: 2026-11-03T02:20:56Z
created_by:
  id: agent:claude-code/t3-0270c158
  name: ""
updated_by:
  id: agent:claude-code/t3-0270c158
  name: ""
extensions: {}
---

## Description

TestTakeIsConsistentUnderAWriter in internal/adapter/sqlitesnap failed once in a full local `GOFLAGS=-mod=mod just ci` on ccb9e4c (load average about 5) with `no snapshot succeeded`: within its 2-second deadline every Take returned ErrChanged, because the writer goroutine commits and checkpoints continuously. The package had not changed since v0.11.1. Alone it passed 20 of 20 with -count=20, and a second full run passed. Seen during the v0.12.0 release gates (TKT-01M6FK6T3EBN2V08Q19M8PMPB9).

The test should prove a consistent snapshot is possible under a writer without depending on a wall-clock window that a loaded machine or the shared CI runner can exhaust: for example, pace the writer, or count only attempts rather than time.
