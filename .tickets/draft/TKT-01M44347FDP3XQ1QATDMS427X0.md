---
schema: 3
id: TKT-01M44347FDP3XQ1QATDMS427X0
title: "Flaky under load: sqlitesnap TestTakeIsConsistentUnderAWriter"
type: bug
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/adapter
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
created_at: 2026-10-04T18:34:24Z
updated_at: 2026-10-04T18:34:24Z
created_by:
  id: agent:claude-code/9078ac3f
  name: ""
updated_by:
  id: agent:claude-code/9078ac3f
  name: ""
extensions: {}
---

## Description

`TestTakeIsConsistentUnderAWriter` (`internal/adapter/sqlitesnap/sqlitesnap_test.go`) failed with "no snapshot succeeded" in two full `just ci` runs on 2026-10-04, on a branch that does not touch the package. It passed 5 of 5 times run alone (`go test -count=5 -run TestTakeIsConsistentUnderAWriter ./internal/adapter/sqlitesnap/`).

The test gives `Take` 2 seconds and 40 attempts against a writer that commits as fast as it can and checkpoints every 7 commits. Under full-suite load every attempt can see the database change and return `ErrChanged`. The package imports no other lampi package, so this is load, not a regression elsewhere.

### Options

- Slow the writer, for example a short sleep between commits, so a snapshot window exists on a busy machine.
- Scale the deadline, or retry until a count of successes rather than a wall-clock limit.
