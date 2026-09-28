---
schema: 3
id: TKT-01M3G3B0ZCVM8NAM4P15YFHHXZ
title: "OIDC: TestProviderFlowAndRotation fails rarely after a key rotation"
type: bug
status: done
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
updated_at: 2026-09-28T19:32:52Z
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

**agent:claude-code/2cf53976** at 2026-09-28T18:13:03Z

Root cause is in go-oidc v3.21.0 (latest; upstream v3 unchanged). RemoteKeySet calls inflight.done(keys) before clearing r.inflight, so a lookup for an unknown key ID in that window joins the finished fetch and gets the pre-rotation keys. In the test, flow 1 signs with the rotated key right after flow 0 verifies and fails with ErrIdentity. The same window exists in production right after an IdP rotation.

Evidence: unfixed with all cores busy, -race -cpu 1,2 -count=1500 gave 25/3000 failures. With 20ms sleep injected after done() in a scratch go-oidc copy (-modfile), 20/20 failed. Fixed: 0/3000 and 0/20; 0/6000 unloaded.

Fix: rotatingKeys in internal/webauth/provider.go wraps the key set. On a verify failure it builds a fresh RemoteKeySet whose first fetch starts after the token arrived, tries once more, and swaps it in on success (guarded against concurrent swaps). The verifier is built with oidc.NewVerifier using the discovered issuer and the same allowed algorithms. TestProviderRotationAfterStaleFetch forces the interleaving deterministically.

Alternatives: retrying the library key set can rejoin the same stale fetch (would need timed backoff); an own JWKS client would duplicate go-oidc's unsupported-key skipping; changing only the test IdP hides a real production race; no fixed go-oidc version to pin. Cost: a genuinely bad token triggers one extra JWKS fetch, bounded by the provider's 8-call limit. Not reported upstream yet.

## Summary

Merged in #86. The flake was a go-oidc v3.21.0 race: a lookup right after a key rotation could join a finished JWKS fetch and get the old keys. rotatingKeys retries once with a fresh key set after a verify failure. This also fixes the production case of a refused login right after an IdP rotation.
