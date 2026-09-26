---
schema: 3
id: TKT-01M3FHHBFAYKEK0NAXVR91969G
title: "Lake identity: ed25519 key list, published keys, signed hello"
type: task
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
  - area/auth
  - area/protocol
assignees: []
milestone: null
parent: TKT-01M3FHHBCJ12FXKNTB6Z138F6N
origin: null
dependencies:
  - TKT-01M3FHHBDS7VCKK5AJ7T3H6DYX
blocks_on: none
references: []
claim:
  actor: agent:claude-code/e4a47e8c
  branch: onboarding/lake-identity
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-e4a47e8c
  commit: 4a3744dafc4fae0345b675502a643ca6b5794948
  session: null
  claimed_at: 2026-09-26T20:23:22Z
  expires_at: null
archive: null
created_at: 2026-09-26T19:02:11Z
updated_at: 2026-09-26T20:31:13Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/e4a47e8c
  name: ""
extensions: {}
---

## Description

Part of the agent onboarding epic. Give the lake a persistent identity, publish it, and let clients prove the lake holds it.

Today `serve` has no keypair and no instance id. `HelloResponse` (`internal/protocol`) carries only `server_time`, `protocol_versions` and `max_blob_bytes`.

### Identity

- On first start, `serve` creates an ed25519 key with a key id, and a random lake id, in `identity.json` in the data directory at mode 0600, and records the lake id in the catalog. Later starts load the file and check it against the recorded id.
- A lake upgrading from a release with no identity has a catalog and no recorded lake id. It gets an identity on its first start, the same as a new lake, so the upgrade order in the epic holds.
- A catalog that has recorded a lake id but whose `identity.json` is missing or holds another lake refuses to start and says to restore the file from backup. Agents pin the key, so a silent new identity would look like a different lake to all of them.
- The store holds a list of keys with status (active or retired) and validity windows from the start, even though this child only ever creates one. The rotation child uses the list.
- `serve backup`, restore and `fsck` cover the keys. A restore onto a fresh data directory keeps the same identity.
- `serve identity` prints the lake id and each key's fingerprint, for the operator to compare with what an agent shows.

### Published keys

`GET /.well-known/terva-lampi/keys?nonce=N` needs no token, like `/healthz`, because an agent that is not yet registered has none. It returns the lake id and each key's id, public key, status and validity window. It is signed by every active key over the document and the caller's nonce, so a copied response cannot be replayed to a new nonce. It returns no catalog data. Bound the nonce length and rate-limit the route. The policy child records that this is a second route without a token.

`hello` also returns the lake id and active key ids, and signs the request nonce and server time, so an agent that is already registered checks its pin on every connection.

## Acceptance criteria

- [x] serve creates the key and lake id once and reloads them on every later start
- [x] hello returns the lake id and public key, plus a signature a client can verify
- [x] Backup and restore keep the lake's identity
- [x] GET /.well-known/terva-lampi/keys returns the lake id and key list without a token, signed over the caller's nonce, with no catalog data
- [x] serve identity prints the lake id and each key fingerprint
- [x] A pre-identity catalog gets an identity on first start, and a catalog that recorded a lake id refuses to start without its matching identity.json

## Implementation plan

New internal/identity package: identity.json (0600) holds a lake id and a list of ed25519 keys with status and not_after; created by link-from-temp so a racing create cannot replace it. Catalog migration 4 adds lake_meta and records lake_id once. identity.Ensure: load and record, or make and record when no lake id is recorded, or refuse when one is recorded and the file is missing or different. Signed envelope {payload raw, signatures[]} over 'terva-lampi/<context>\0'+payload bytes, contexts keys/v1 and hello/v1. GET /.well-known/terva-lampi/keys?nonce= (open, global token-bucket 20 burst 5/s, no-store); hello takes an optional nonce and returns lake_id and a proof. serve ensures the identity at start and prints it; serve identity prints fingerprints; backup copies identity.json after the catalog; fsck loads it.

## Notes

**agent:claude-code/e4a47e8c** at 2026-09-26T20:25:04Z

Supersedes the startup rule as first filed ('a data directory with a catalog but no key is an error'). That rule would have stopped the hosted lake on its first upgrade, since every existing catalog lacks a key; terva-review 891 on PR #7 flagged the same. The catalog now records the lake id when an identity is first made, and only a recorded id with no matching identity.json refuses to start. Rejected: an explicit 'serve identity init' step before upgrade, because it adds an operator step that, if forgotten, leaves the lake without a key endpoint and blocks every registration.

**agent:claude-code/e4a47e8c** at 2026-09-26T20:31:13Z

Criterion 2 as met: hello carries the lake id and a proof signed by every active key, whose key_id names the key. It does not repeat the public key itself; a client gets that from the key list, which is where it pins it. Rate limit is global rather than per address because behind Caddy every caller has the proxy's address. Evidence: internal/identity tests (Ensure's four cases, create refusing to replace, sign/verify with context separation and tamper), internal/api/identity_test.go (route open, signed, no catalog words, 404 without identity, 429, web bypass, old {} hello), internal/cli/identity_test.go (backup and restore keep it, restore without it refuses), and TestGoLiveRestore (tag golive), whose restored serve starts only because the backup carried identity.json. go test -race ./... green.
