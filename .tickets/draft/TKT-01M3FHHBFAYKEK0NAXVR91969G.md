---
schema: 3
id: TKT-01M3FHHBFAYKEK0NAXVR91969G
title: "Lake identity: persistent ed25519 key and lake id, signed hello"
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
updated_at: 2026-09-26T19:02:11Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/e4a47e8c
  name: ""
extensions: {}
---

## Description

Part of the agent onboarding epic. Give the lake a persistent identity that registration codes and base configuration can be signed with.

Today `serve` has no keypair and no instance id. `HelloResponse` (`internal/protocol`) carries only `server_time`, `protocol_versions` and `max_blob_bytes`.

- On first start, `serve` creates an ed25519 key and a random lake id in the data directory, at mode 0600. Later starts load them. A missing key on a data directory that already holds a catalog is an error, not a silent new identity, because agents pin the key.
- `hello` returns the lake id and the public key. Add a signed field so a client can check that the answering lake holds the private key, for example a signature over the request nonce and the server time.
- `serve backup`, restore, and `fsck` cover the key. A restore onto a fresh data directory keeps the same identity.
- Rotating the key is out of scope here. Say so in the docs, and say what an agent does when the key changes (it refuses the lake and says why).

## Acceptance criteria

- [ ] serve creates the key and lake id once and reloads them on every later start
- [ ] hello returns the lake id and public key, plus a signature a client can verify
- [ ] A data directory with a catalog but no key refuses to start
- [ ] Backup and restore keep the lake's identity
