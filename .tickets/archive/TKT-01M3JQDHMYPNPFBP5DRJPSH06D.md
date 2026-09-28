---
schema: 3
id: TKT-01M3JQDHMYPNPFBP5DRJPSH06D
title: "webauth: TestProviderFlowAndRotation flakes after a key rotation"
type: bug
status: archived
status_reason: null
priority: low
due_on: null
labels:
  - area/auth
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive:
  archived_at: 2026-09-28T18:00:01Z
  from_status: draft
  reason: duplicate
created_at: 2026-09-28T00:42:41Z
updated_at: 2026-09-28T18:00:01Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/2cf53976
  name: ""
extensions: {}
---

## Description

`TestProviderFlowAndRotation` in internal/webauth/provider_test.go failed once in CI (Forgejo actions run 499, on PR #47, 2026-09-28) with `provider_test.go:33: flow 1: identity response did not verify`. That is the exchange just after `testidp.Server.Rotate()`. It passed 200 of 200 runs locally, so it is intermittent.

The test IdP's ES256 encoding pads r and s with FillBytes, so that is not the cause. A likely suspect is go-oidc's remote key set: after a rotation the new kid is unknown, and the verifier's refetch of the JWKS may race or be rate-limited. Reproduce with `go test ./internal/webauth -run TestProviderFlowAndRotation -count=2000 -race`, and check how the provider's key set refreshes on an unknown kid.

## Notes

**agent:claude-code/2cf53976** at 2026-09-28T17:59:55Z

Duplicate of TKT-01M3G3B0, which has the earlier analysis. The evidence from this ticket is copied there. Archived.

**agent:claude-code/2cf53976** at 2026-09-28T18:00:01Z

archived from draft: duplicate
