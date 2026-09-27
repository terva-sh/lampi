---
schema: 3
id: TKT-01M3FHHBJAZ54CXQ7J1Y88XGVF
title: "Registration codes: mint on the lake host, redeem over /v1/register"
type: task
status: done
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
updated_at: 2026-09-27T01:01:02Z
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

- [x] serve register prints a signed, versioned code that holds the URL, lake id, key, one-time secret and expiry
- [x] A code redeems once. A used, expired or revoked code is refused
- [x] The lake stores only hashes of the secret and of the device token
- [x] /v1/register is rate-limited, and its logs never contain the secret
- [x] docs/protocol.md documents the code format and the route

## Implementation plan

regcode package: tlr1.<b64url payload>.<b64url sig>, payload {v,url,lake_id,key_id,public_key,secret,expires}, ed25519 by the first active key over context registration/v1 (identity.SignRaw/VerifyRaw); Decode checks every field and the signature against the embedded key, not expiry. Catalog migration 6: registrations(id reg_…, name, secret_sha256 UNIQUE, profile, created/expires/used/revoked, device_id); public_url in lake_meta. CreateRegistration refuses a name held by a device or a pending code; Redeem spends the code and inserts a registration-source device bound to the machine in one transaction, refusing unknown/used/expired/revoked, a token hash or machine another device holds. API: POST /v1/register open, shares openLimit, 4 KiB body, one 403 for all code refusals with the reason in audit only, 409 for a bound machine and for a lake without --token-file; response has device id, name, lake id and the signed profile. authed accepts a token that is not in the token file only when its catalog device is from registration. CLI: serve identity set-url (https or loopback http, stored in catalog, shown by serve identity); serve register --name/--expires/--profile checks the URL serves this lake's key list over a fresh nonce before minting, prints only the code on stdout; --list and --revoke; audit for create/redeem/revoke/refuse.

## Notes

**agent:claude-code/e4a47e8c** at 2026-09-26T21:47:34Z

Decisions:

- **Public URL storage.** The URL is kept in the catalog's `lake_meta` rather than in a loose file. That way a backup covers it with no extra copy step, and `serve identity` can show it. The ticket said "in the data directory", and the catalog is there.
- **One signature, no envelope.** The code carries a single bare signature. An envelope with one signature per active key would roughly double the code's length for nothing: the agent pins one key.
- **Expiry and naming.** Expiry is capped at 30 days, and the default is 24h as the policy says. The name the operator gives wins, and the agent's suggested name goes to the audit log only.
  - Rejected: letting the suggested name decide. It would let whoever holds a code choose the device name an operator sees in `serve devices list`.
  - A name taken by a token-file device between mint and redeem gets a suffix, not a refusal.
- **Registration needs `--token-file`.** On a lake started without one, the first registered device would switch the lake from open to token-required and lock out every client using it without a token. The route answers 409 there instead.
- **Registered tokens are not added to the in-memory token set.** `authed` hashes the bearer and reads the catalog, which it already did for every request. The catalog stays the single source for registered devices, and a revoke lands on the next request. The cost is one catalog lookup for each request with a wrong token, and those are rate-limited only by the proxy.
- **One refusal message.** Unknown, used, expired and revoked codes, and a replayed token hash, all get the same 403 text, so the route cannot be used to probe which codes exist. The audit log keeps the reason and the registration id. It never records the secret.
- **A refused redeem leaves the code usable.** When the machine is bound to another device, or the token hash is already taken, the code stays pending. The operator can unbind and the same code still works.
- **Mint checks the public URL first.** Minting fetches the key list through the stored public URL with a fresh nonce and checks both the signature and the nonce. This catches a URL that points at another lake, and a proxy that does not forward `/.well-known`, before a code goes out. `TestServeRegisterMintsACodeThatRedeemsOnce` covers the other-lake case.

Evidence:

- `TestEncodeDecode` (regcode)
- `TestRegistrationRedeemsOnce` (catalog)
- `TestRegisterRedeemsACodeOnceAndTheTokenWorks`, `TestRegisterRefusesExpiredRevokedAndOpenLakes`, `TestRegisterSharesTheOpenRateLimit` (api). These cover the token working, binding, a revoked registered device, and no secret or token in the access log or the audit log.
- `TestServeRegisterMintsACodeThatRedeemsOnce` (cli)
- The full `-race` suite, the golive drills, and the `GOOS=windows` vet are green.

The backup covers pending codes because they live in `catalog.db`, which the backup already copies with VACUUM INTO. There is no separate test for that.

**agent:claude-code/e4a47e8c** at 2026-09-27T00:04:01Z

Review 932 on PR #17, all accepted. 40b6fab: every refusal the limiter lets through writes registration.refused with a fixed lake-side reason, never request text. Rate-limited 429s are deliberately not audited: each audit line is a synced write, and the limiter is the only bound on writes an open route can cause. 698a449: revoke is one immediate transaction that only changes a still-pending code, so a revoke racing a redeem can no longer both succeed. c7b7547: serve register writes the mint to the audit log before printing; on a failed write the code is revoked and never shown.

**agent:claude-code/e4a47e8c** at 2026-09-27T01:01:02Z

Review 974 on PR #17, both accepted. 195a350: a second revoke of a code returns ErrRegistrationRevoked, and serve register --revoke says it was already revoked and writes no second audit event. e07eb3a: a body read error on /v1/register answers 400 through bodyStatus rather than an empty 200. The same gap in /v1/hello, from TKT-01M3FHHBF, is filed as TKT-01M3G44GC.

## Summary

serve identity set-url stores the lake's public URL; serve register mints signed tlr1 codes (URL, lake id, key, one-time secret, expiry) after checking the URL serves this lake, and lists and revokes them. POST /v1/register redeems a code once into a machine-bound registration device and returns the signed profile; the lake keeps only hashes, refuses used/expired/revoked codes with one message, rate-limits, and audits every event without the secret.
