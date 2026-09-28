---
schema: 3
id: TKT-01M3MKF5GVKFA0SRRZFF9RPB6X
title: Flaky TestSlowLinkUploadsBlobAtCap under CPU load
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
created_at: 2026-09-28T18:12:09Z
updated_at: 2026-09-28T22:08:12Z
created_by:
  id: agent:claude-code/2cf53976
  name: ""
updated_by:
  id: agent:claude-code/aa1afd80
  name: ""
extensions: {}
---

## Description

TestSlowLinkUploadsBlobAtCap (internal/upload/transport_test.go:104) failed once in just ci with 'no bytes moved for 200ms' while the host load average was ~12.7 from parallel race loops. 20 of 20 reruns passed on a quieter host. The 200ms stall window is tight enough that scheduler delay under load trips it.

## Notes

**agent:claude-code/aa1afd80** at 2026-09-28T22:08:12Z

Failed again on the Forgejo runner in PR #105 (Actions run 1000, job 0), 2026-09-28: 'no bytes moved for 200ms' at transport_test.go:104. PR #105 doesn't touch internal/upload, and the test passed 20 of 20 runs locally with -count=20.
