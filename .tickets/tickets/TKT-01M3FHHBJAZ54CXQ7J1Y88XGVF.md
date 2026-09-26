---
schema: 3
id: TKT-01M3FHHBJAZ54CXQ7J1Y88XGVF
title: "Registration codes: mint on the lake host, redeem over /v1/register"
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/auth
  - area/server
  - area/protocol
assignees: []
milestone: null
parent: TKT-01M3FHHBCJ12FXKNTB6Z138F6N
origin: null
dependencies:
  - TKT-01M3FHHBFAYKEK0NAXVR91969G
  - TKT-01M3FHHBGQ3YV3PP22HPDDNKJM
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-26T19:02:11Z
updated_at: 2026-09-26T20:20:46Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/e4a47e8c
  name: ""
extensions: {}
---

## Description

Part of the agent onboarding epic. Mint registration codes on the lake host and redeem them over the capture protocol.

### Minting

`terva-lampi serve register --name NAME [--expires 24h] [--profile P]` writes a pending registration to the lake's store and prints the code on stdout. It needs the lake's public URL. The operator sets it once with `serve identity set-url URL`, which stores it in the data directory, never in git. Minting fetches `/.well-known/terva-lampi/keys` through that URL and refuses when it does not reach this lake, which catches a wrong URL or a proxy that does not forward the route before the code goes out. The code is versioned and URL-safe. It holds the format version, the lake URL, the lake id, the lake public key, the one-time secret, the expiry, and an ed25519 signature over all of those.

### Redeeming

`POST /v1/register` is the one `/v1` route without a bearer token. The body is the one-time secret, the device token hash the agent generated, a machine id and a suggested name. The lake checks the secret against the pending registration, refuses one that is used, expired or revoked, and creates the device from the named-device child. The response is the device id and the signed base configuration. Store only a hash of the secret. Rate-limit the route. A failed attempt is logged without the secret.

`serve register --list` and `--revoke` manage pending codes. The code's profile is recorded on the device it creates.

The lake is the authority on expiry. Creation, redemption, expiry, revocation and every refused attempt go to the audit log from the named-devices child. Pending codes bump the catalog schema version if they live in the catalog, and backup covers them.

## Acceptance criteria

- [ ] serve register prints a signed, versioned code that holds the URL, lake id, key, one-time secret and expiry
- [ ] A code redeems once. A used, expired or revoked code is refused
- [ ] The lake stores only hashes of the secret and of the device token
- [ ] /v1/register is rate-limited, and its logs never contain the secret
- [ ] docs/protocol.md documents the code format and the route
