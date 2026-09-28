---
schema: 3
id: TKT-01M3G3B0ZCVM8NAM4P15YFHHXZ
title: "OIDC: TestProviderFlowAndRotation fails rarely after a key rotation"
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
created_at: 2026-09-27T00:13:18Z
updated_at: 2026-09-28T17:59:55Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/2cf53976
  name: ""
extensions: {}
---

## Description

`TestProviderFlowAndRotation` in internal/webauth/provider_test.go failed once on the CI runner (PR #20, head 5bf3567, run for commit 5bf3567c) with `provider_test.go:33: flow 1: identity response did not verify`. That is `ErrIdentity`, raised on the exchange right after `s.Rotate()`. The same test passed 200 times in a row locally with `-race`, and neither internal/webauth nor internal/testidp is touched by the onboarding stack, so this looks like a rare flake.

Ruled out: short JWK coordinates. internal/testidp pads x and y to 32 bytes with `FillBytes`, and the signature halves the same way.

Worth checking next: how the provider refreshes its key set when it sees an unknown kid after a rotation (caching or rate-limiting of the JWKS refetch), and whether the old key can still be served while a new token is signed with the new one. testidp's Rotate swaps the key and kid under s.mu.

## Notes

**agent:claude-code/2cf53976** at 2026-09-28T17:59:55Z

More evidence (2026-09-28), filed in TKT-01M3MJ178F before this ticket was found: failed again on Forgejo CI run 793 (PR #80, which does not touch webauth) with 'flow 1: identity response did not verify'. TLS handshake errors against the test servers came just before it. It passed 30 of 30 runs with -race locally. TKT-01M3JQDH is another duplicate of this one.

**agent:claude-code/2cf53976** at 2026-09-28T17:59:55Z

More evidence (2026-09-28), filed in TKT-01M3JQDH before this ticket was found: (from TKT-01M3JQDH) failed in Forgejo run 499 on PR #47. The suggested reproduction is go test ./internal/webauth -run TestProviderFlowAndRotation -count=2000 -race.
