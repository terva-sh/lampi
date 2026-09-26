---
schema: 3
id: TKT-01M3FHHBFAYKEK0NAXVR91969G
title: "Lake identity: ed25519 key list, published keys, signed hello"
type: task
status: draft
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
claim: null
archive: null
created_at: 2026-09-26T19:02:11Z
updated_at: 2026-09-26T19:41:23Z
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

- On first start, `serve` creates an ed25519 key with a key id, and a random lake id, in the data directory at mode 0600. Later starts load them. A data directory that already holds a catalog but has no key is an error, not a silent new identity, because agents pin the key.
- The store holds a list of keys with status (active or retired) and validity windows from the start, even though this child only ever creates one. The rotation child uses the list.
- `serve backup`, restore and `fsck` cover the keys. A restore onto a fresh data directory keeps the same identity.
- `serve identity` prints the lake id and each key's fingerprint, for the operator to compare with what an agent shows.

### Published keys

`GET /.well-known/terva-lampi/keys?nonce=N` needs no token, like `/healthz`, because an agent that is not yet registered has none. It returns the lake id and each key's id, public key, status and validity window. It is signed by every active key over the document and the caller's nonce, so a copied response cannot be replayed to a new nonce. It returns no catalog data. Bound the nonce length and rate-limit the route. The policy child records that this is a second route without a token.

`hello` also returns the lake id and active key ids, and signs the request nonce and server time, so an agent that is already registered checks its pin on every connection.

## Acceptance criteria

- [ ] serve creates the key and lake id once and reloads them on every later start
- [ ] hello returns the lake id and public key, plus a signature a client can verify
- [ ] A data directory with a catalog but no key refuses to start
- [ ] Backup and restore keep the lake's identity
- [ ] GET /.well-known/terva-lampi/keys returns the lake id and key list without a token, signed over the caller's nonce, with no catalog data
- [ ] serve identity prints the lake id and each key fingerprint
