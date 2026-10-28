---
schema: 3
id: TKT-01M626C0YFTDT3KAKEH021TPVE
title: "Flaky TestDiagnosisRoutes: Since differs across a second"
type: bug
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-28T21:24:00Z
updated_at: 2026-10-28T21:24:00Z
created_by:
  id: agent:claude-code/d4b24133
  name: ""
updated_by:
  id: agent:claude-code/d4b24133
  name: ""
extensions: {}
---

## Description

`TestDiagnosisRoutes` in `internal/web/diagnosis_test.go` asks for the lake-wide diagnosis three ways, over the read API, the browser API and the `diagnose_lake` MCP tool, then compares them with `reflect.DeepEqual`. It blanks `AsOf` on each before comparing but not `Since`, which is also derived from the current time (seven days back by default). When the three calls straddle a second boundary, the reports differ only in `Since` and the test fails:

```
diagnosis_test.go:103: lake surfaces differ:
    {Since:2026-10-21T21:18:34Z Limit:5 ...
    {Since:2026-10-21T21:18:35Z Limit:5 ...
```

This failed Lint and Test on #218 on 2026-10-28, a change that does not touch diagnosis. A rerun passes.

### Fix

Blank `Since` along with `AsOf`, or pass an explicit `since` on all three calls so the reports are compared on the same window.

## Acceptance criteria

- [ ] The three lake reports are compared with Since fixed or blanked
