---
schema: 3
id: TKT-01M3FHHBDS7VCKK5AJ7T3H6DYX
title: "Policy: allow registration codes, named devices and many lakes"
type: task
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area/docs
  - policy
  - area/auth
assignees: []
milestone: null
parent: TKT-01M3FHHBCJ12FXKNTB6Z138F6N
origin: null
dependencies: []
blocks_on: none
references: []
claim:
  actor: agent:claude-code/e4a47e8c
  branch: t3code/review-tickets-agent-deployment
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-e4a47e8c
  commit: 3c5a73063f1ba1c4364904e44c8ae36d9eb25a3c
  session: null
  claimed_at: 2026-09-26T20:20:57Z
  expires_at: null
archive: null
created_at: 2026-09-26T19:02:11Z
updated_at: 2026-09-26T20:22:02Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/e4a47e8c
  name: ""
extensions: {}
---

## Description

Part of the agent onboarding epic. Amend the Phase 0 policy so that the recorded decision matches the work that follows.

`docs/policy.md` (the Lake host section), `docs/architecture.md` (Auth and where the bytes sit) and `docs/vps-bringup.md` (Device token) each say "There is no enrolment API". `docs/protocol.md` describes auth as one anonymous token per device. Replace these with the registration model from the epic: lake identity key, named devices, one-time registration codes, lake-supplied base configuration, and agents that report to several lakes. Keep what still holds: TLS in front of `serve`, no secret as a command argument, no hostname in git, and no TTL on session data.

Write the threat model in `docs/policy.md`: what a leaked code allows (one registration within its expiry), what a stolen device token allows (uploads as that device until it is revoked), what the key pin protects against (a different lake answering at the same URL), and and what it does not protect against. The published key list and nonce signature defeat a code with the right URL and a wrong key, a retired key, and a replayed key list. They do not defeat a forged code that points at an attacker's own URL, which only the fingerprint confirmation catches. Record that `/.well-known/terva-lampi/keys` and `/v1/register` join `/healthz` as routes that need no token, and what each one exposes.

Also record the upgrade order (lake first, protocol stays 1 while changes are additive), that Windows needs an agent restart to add or remove a lake, and that registrations, revocations, key changes and refused redemptions go to an append-only audit log on the lake.

This child is docs only. The original decision is recorded as the owner's. They confirmed the replacement decisions on 2026-09-27, and they review the policy text in this child's PR.

## Acceptance criteria

- [x] policy.md, architecture.md, protocol.md and vps-bringup.md no longer say there is no enrolment API, and they describe the registration model
- [x] policy.md states the threat model for leaked codes, stolen tokens and the key pin
- [ ] The owner has approved the policy text

## Implementation plan

Docs only. Add a 'Registration and many lakes' section to docs/policy.md with the model, the routes that need no token, a threat table, audit, upgrade order and Windows. Replace 'There is no enrolment API' in policy, architecture and vps-bringup with a pointer to it that keeps the manual token copy as the fallback. Add a short forward note in protocol.md; each route is documented there when it lands.
