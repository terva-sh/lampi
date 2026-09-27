---
schema: 3
id: TKT-01M3FKS3XHYM1QXR4Q56SGWY6Y
title: "Lake key rotation: chained keys, overlap, retire and compromise"
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/auth
  - area/server
  - area/agent
assignees: []
milestone: null
parent: TKT-01M3FHHBCJ12FXKNTB6Z138F6N
origin: null
dependencies:
  - TKT-01M3FHHBFAYKEK0NAXVR91969G
  - TKT-01M3FHHBKTMJCHCMQQQJZKQTMF
  - TKT-01M3FHHBN7G90MR0BJ89DGPRQQ
  - TKT-01M3FP1107KXYARCAYVYT2Y409
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-26T19:41:23Z
updated_at: 2026-09-27T02:14:07Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/e4a47e8c
  name: ""
extensions: {}
---

## Description

Part of the agent onboarding epic. Let a lake replace its signing key without re-registering its agents, and let it retire a key it believes is compromised.

The identity child stores a key list with status and validity windows but only ever creates one key. This child adds rotation on top of that list and the published key endpoint.

- `serve identity rotate` adds a new active key. The new key's entry is signed by the current key, so an agent can chain from its pin. Both keys stay active for an overlap window. `serve identity retire KEY-ID` ends a key's window early.
- The agent refetches the key list on start and with the base configuration. It accepts a new key only when it chains to its pinned key, then moves the pin. A list that drops the pinned key with no chain to a new one is refused, and the agent keeps uploading only while its pinned key is still active.
- Codes signed by a retired key are refused at registration, both by the lake and by the key-list check in `register`.
- Compromise: `serve identity retire --compromised KEY-ID` retires the key with no chain. Agents pinned only to it stop and say that the lake must be re-registered with `--replace`. The docs say what an operator does next.

## Acceptance criteria

- [x] serve identity rotate adds a key signed by the current one, and agents move their pin without re-registering
- [x] A key list with no chain to the pinned key is refused, and the agent names the reason
- [x] Codes signed by a retired key are refused by the lake and by register
- [x] retire --compromised stops pinned agents and the docs give the recovery steps

## Implementation plan

identity: keys carry endorsed_by, endorsement (ed25519 by the previous current key over {lake_id,key_id,public_key}, context key-endorse/v1) and compromised; Current() = newest active key (codes and rotation use it); Rotate adds an endorsed key and ends the other active windows at now+overlap; Retire ends a window, refuses the last active key, optionally marks compromised; Save replaces identity.json atomically. VerifyKeyList authenticates a published list by any key chained from the pin (ignoring compromised marks); Advance then follows endorsements from the pin, skipping compromised keys, to the newest active key, or returns ErrPinCompromised / ErrNoChain. serve identity rotate/retire [--compromised] write identity.json and audit key.added/key.retired; serve SIGHUP reloads identity.json (Server identity is now an atomic pointer). Registrations record the signing key; the lake refuses a code whose key is no longer active; register checks the key's status in the list before its signature. Clients: upload.Options.Pin makes every hello carry a nonce and requires lake_id and a proof by the pinned key, else PinError and nothing is pushed. refreshPin (agent at start and hourly in the profile loop, sync before each pinned lake) fetches the list, verifies, advances, refetches the profile under the new pin, then writes the pin to config.json; the agent reloads the lake.

## Notes

**agent:claude-code/e4a47e8c** at 2026-09-26T22:09:24Z

Decisions:

- **Every push checks the pin.** Every push now verifies the hello proof against the pin. The policy already promised this, and nothing did it before this change. It is what actually stops uploads when a pin is compromised or broken. A refusal from the key list alone is advisory, because a man in the middle could suppress the list.
- **List signature before compromise marks.** A list is authenticated before its compromised marks are believed, because the marks are only as good as the signature. VerifyKeyList follows the chain while ignoring marks; Advance then honours them.
  - Rejected: acting on marks in an unverified list. Anyone could then stop an agent by serving `compromised: true`. That would only deny service, but it is still wrong.
- **Profile under the new pin.** When a pin moves, refreshPin fetches and verifies the profile under the new pin before writing config.json. The first rotation test found the gap: the cached profile carried only the old key's signature, so the lake's allowlist dropped out after the move.
- **serve reloads identity on SIGHUP.** Server.Identity became an atomic pointer behind Identity()/SetIdentity() so that serve can reload identity.json on SIGHUP. The race detector caught a test swapping it under live handlers. The only alternative was requiring a serve restart for every rotation.
- **Signing key recorded on registrations.** Migration 6 (registrations) gained key_id in place instead of adding migration 7. Migration 6 has not shipped: it exists only on this unmerged branch stack, and the hosted lake runs an earlier release. If that changes before merge, this must become a new migration.
- **Downgrade cost.** identity.json gains optional fields. Load uses DisallowUnknownFields, so after a rotation an older serve binary refuses the file. A downgrade after rotating therefore needs identity.json from a backup taken before the rotation.
- **Default overlap.** The default overlap is 14 days. Agents read the list hourly while running, and the retired key stays listed with its endorsement, so a machine that was off longer still follows the chain.

Evidence:

- TestRotateChainsAndRetireCompromisedBreaksIt (identity): chain, overlap, load round trip, stranger pin, forged endorsement, compromise.
- TestRotationMovesThePinAndCompromiseStopsIt (sync: move, retire old, compromise of the pinned key stops with the register --replace message, audit).
- TestCodesFromARetiredKeyAreRefused (register check 3 and the lake's own 403 with reason=signing key retired).
- TestARunningAgentFollowsARotation (agent moves the pin and restarts the lake).
- TestAgentRefusesALakeWithAnotherKeyAndKeepsItsCache (another lake at the URL: key list refused and every hello refused by the pin, nothing pushed).
- TestServeReloadsIdentityOnHangup.
- Full -race suite, golive and windows vet are green.

AC2 is backed by two kinds of evidence. The ErrNoChain path is unit-tested; the agent-level refusal is exercised through the lake-id mismatch and the pin check.

**agent:claude-code/e4a47e8c** at 2026-09-27T00:04:28Z

Review 934 on PR #19. (1) accepted, a6091fb: the pin stays on the old key when the profile under the new key cannot be fetched, and the next refresh retries. Trade-off: a device whose profile the lake drops for good cannot move its pin, and it loses hello once the old key's overlap ends, until the profile is restored. That is preferred to a pin with no verified profile behind it. (2) rejected: the lake signs its key list only with keys inside their window (ActiveKeys applies NotAfter, the same rule the lake uses to accept a code), so register already refuses a code from an expired key at the key-list check. Reproduced with rotate --overlap 0s. A check against the client's clock would only add skew failures.

**agent:claude-code/e4a47e8c** at 2026-09-27T01:01:03Z

Review 975 on PR #19, accepted in 56cd134. refreshPin keeps a copy of the cached profile before refetching under the new key, and puts it back (or removes a new one) when the moved pin cannot be written. The cache and the pin therefore never disagree. It only mattered after the old key was retired, when the lake signs with the new key alone.

**agent:claude-code/e4a47e8c** at 2026-09-27T01:40:49Z

Review 986 on PR #19, accepted in 2f77bac. The pin is written through config.UpdateLake, which refuses when the stored lake id, key id or public key differ from the ones the refresh started with, and restores the old cached profile. After the lake-profile layer made reload report success, a moved pin now waits on the same pending reload as a new profile: it is retried each tick, and the first push is held until it succeeds (merge commit on this layer). Remaining gap: config.json has no lock between processes; filed as a draft.

**agent:claude-code/e4a47e8c** at 2026-09-27T02:14:07Z

Review 991 on PR #19, accepted in 8c4da6a. The pin is not written into an entry whose server URL changed during the refresh, when the server came from config.json; an override was never the entry's URL, so nothing is compared then.

## Summary

serve identity rotate adds an endorsed key with an overlap; retire ends a key, --compromised marks it. Agents and sync follow endorsements from their pin (authenticated list first, compromise marks second), move the pin in config.json and refresh the profile under it; every hello must prove the pinned key or nothing is pushed. Codes from a retired key are refused by the lake and by register. serve reloads identity.json on SIGHUP. docs/policy.md has the compromise recovery steps.
