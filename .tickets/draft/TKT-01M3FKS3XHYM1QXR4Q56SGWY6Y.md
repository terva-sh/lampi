---
schema: 3
id: TKT-01M3FKS3XHYM1QXR4Q56SGWY6Y
title: "Lake key rotation: chained keys, overlap, retire and compromise"
type: task
status: draft
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
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-26T19:41:23Z
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

Part of the agent onboarding epic. Let a lake replace its signing key without re-registering its agents, and let it retire a key it believes is compromised.

The identity child stores a key list with status and validity windows but only ever creates one key. This child adds rotation on top of that list and the published key endpoint.

- `serve identity rotate` adds a new active key. The new key's entry is signed by the current key, so an agent can chain from its pin. Both keys stay active for an overlap window. `serve identity retire KEY-ID` ends a key's window early.
- The agent refetches the key list on start and with the base configuration. It accepts a new key only when it chains to its pinned key, then moves the pin. A list that drops the pinned key with no chain to a new one is refused, and the agent keeps uploading only while its pinned key is still active.
- Codes signed by a retired key are refused at registration, both by the lake and by the key-list check in `register`.
- Compromise: `serve identity retire --compromised KEY-ID` retires the key with no chain. Agents pinned only to it stop and say that the lake must be re-registered with `--replace`. The docs say what an operator does next.

## Acceptance criteria

- [ ] serve identity rotate adds a key signed by the current one, and agents move their pin without re-registering
- [ ] A key list with no chain to the pinned key is refused, and the agent names the reason
- [ ] Codes signed by a retired key are refused by the lake and by register
- [ ] retire --compromised stops pinned agents and the docs give the recovery steps
