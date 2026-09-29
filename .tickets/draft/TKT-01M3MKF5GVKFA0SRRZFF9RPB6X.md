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
updated_at: 2026-09-29T16:08:34Z
created_by:
  id: agent:claude-code/2cf53976
  name: ""
updated_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
extensions: {}
---

## Description

TestSlowLinkUploadsBlobAtCap (internal/upload/transport_test.go:104) failed once in just ci with 'no bytes moved for 200ms' while the host load average was ~12.7 from parallel race loops. 20 of 20 reruns passed on a quieter host. The 200ms stall window is tight enough that scheduler delay under load trips it.

## Notes

**agent:claude-code/aa1afd80** at 2026-09-28T22:08:12Z

Failed again on the Forgejo runner in PR #105 (Actions run 1000, job 0), 2026-09-28: 'no bytes moved for 200ms' at transport_test.go:104. PR #105 doesn't touch internal/upload, and the test passed 20 of 20 runs locally with -count=20.

**agent:claude-code/cd41c9ac** at 2026-09-29T16:08:34Z

Another occurrence, 2026-09-29: Forgejo CI run 1441 (PR #151, a web-only change) failed Lint and Test on this test alone: transport_test.go:104 'upload: PUT /v1/blobs/…: no bytes moved for 200ms' after 21.71s; internal/upload took 272s and internal/web 207s in that run, so the runner was heavily loaded. The same tree passed just ci locally.
