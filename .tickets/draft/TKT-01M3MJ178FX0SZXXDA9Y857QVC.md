---
schema: 3
id: TKT-01M3MJ178FX0SZXXDA9Y857QVC
title: "Flaky under load: webauth TestProviderFlowAndRotation fails to verify"
type: bug
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/auth
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
created_at: 2026-09-28T17:47:03Z
updated_at: 2026-09-28T17:59:55Z
created_by:
  id: agent:claude-code/2cf53976
  name: ""
updated_by:
  id: agent:claude-code/2cf53976
  name: ""
extensions: {}
---

## Description

`TestProviderFlowAndRotation` (`internal/webauth/provider_test.go:33`) failed on Forgejo CI run 793 on 2026-09-28 with `flow 1: identity response did not verify`. That run was for self-update PR #80, which does not touch `internal/webauth`. The log also shows TLS handshake errors against the test servers just before the failure.

It passed 30 of 30 runs with `-race` locally, so it fails only under runner load. Like TKT-01M3MC40FF (recall reindex test nears the 10m test timeout) and TKT-01M3MD4Q1C (TestAgentCancelSkipsFailedSyncRetry sees one hello), it makes CI red on unrelated PRs.

## Acceptance criteria

- [ ] The test passes reliably on the CI runner

## Notes

**agent:claude-code/2cf53976** at 2026-09-28T17:59:55Z

Duplicate of TKT-01M3G3B0, which has the earlier analysis. The evidence from this ticket is copied there. Archived.
