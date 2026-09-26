---
schema: 3
id: TKT-01M3FHHBCJ12FXKNTB6Z138F6N
title: "Agent onboarding: registration codes, lake config, many lakes"
type: epic
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/auth
  - area/agent
  - area/server
  - area/ops
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
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

### Outcome

Once a lake is running, a new agent can be stood up in one of two ways, with no hand-copied token file and no hand-written config:

1. **From a registration code.** The lake host mints a code that carries the lake's location, a one-time registration secret, and the lake's signature. Initialising an agent with that code registers it as a named device and fetches the lake's base configuration.
2. **Standalone, then registered.** An agent is installed and started with no lake. It watches and holds. Entering a registration code later adds the lake, and the running agent starts uploading to it without a restart.

One agent can report to several lakes. Each lake has its own token, its own allowlist and its own sync state, so a session sent to one lake says nothing about whether it has been sent to another.

### Why this is an epic and not a flag

Today the client assumes one lake everywhere: one `server`, one token file, one `machine.json`, and a state directory whose `watermarks.db`, `outbox.db`, `last_sync.json` and `agent.pid` have no lake key. `watermarks.db` even stores the lake's head size as an offset, so lake-specific knowledge already sits in a shared store. The lake has no identity to sign with (`hello` returns only time, protocol versions and the blob cap), and its device tokens are anonymous hashes with no name, so a registered device could not be listed or revoked by name. Each of those is a child below.

### Policy change

`docs/policy.md`, `docs/architecture.md` and `docs/vps-bringup.md` record "There is no enrolment API" as a Phase 0 decision locked by `human:sothr`, and the hardening epic TKT-01M3B35J (Pre-deploy hardening pass) listed it as out of scope. The owner asked for this epic on 2026-09-27, which reopens that decision. The first child amends the policy before any code lands, so the recorded decision and the code do not disagree.

### Design decisions, with the alternatives

These are recommendations made while filing. Each child restates the one it depends on. The owner should confirm or change them before the first child is promoted.

- **The code carries a one-time registration secret, not a device token.** A code is pasted into terminals and chat, so it will leak more often than a 0600 file. A single-use secret with a short expiry (default 24h) limits a leak to one registration within the window. The agent generates its device token locally and sends only its SHA-256, which is what the lake already stores, so the device token never crosses the wire and never appears in the code. Rejected: embedding a ready device token, because a leaked code would then be a permanent credential until someone noticed and revoked it.
- **The signature is the lake's ed25519 key, and the lake publishes its keys.** The code carries the key id and public key that signed it. The lake serves its key list without a token at `GET /.well-known/terva-lampi/keys?nonce=N`: lake id, and each key with its id, status (active or retired) and validity window. The response is signed by every active key over the list and the caller's nonce, because an agent that is not yet registered has no token for `hello`. Before it redeems a code, the agent checks three things. The code's signature verifies against the key it carries. That key is listed as active at the code's URL, fetched over TLS. The response to the agent's own fresh nonce carries a valid signature by that key. The agent then pins the lake id and key, verifies base configuration and later `hello` responses against it, and refuses a lake that answers at the same URL with a key it cannot chain to the pin.

  What this buys at first contact: the key is vouched for by the TLS certificate of the code's URL, not by the code itself. A code that carries the right URL and a wrong key is refused. A code signed by a retired key is refused. A relay that replays a copied list fails, because it cannot sign a new nonce. What it does not buy: a forged code that points at an attacker's own URL passes, because that host serves its own list and signs its own challenge. For that case `register` shows the URL, lake id and key fingerprint and asks for confirmation, and `serve register` prints the same fingerprint for the operator to compare, as with an SSH host key.

  Rejected: trusting the key embedded in the code alone, which is self-asserted and pins whatever the code says. Rejected: publishing the key list without a signed challenge, because a relay can serve a copy. Rejected: an HMAC, which the agent cannot verify at all. Rejected: no signature, which gives no pin against a lake replaced behind the same hostname.
- **Registration is entered through stdin, a prompt or a file, never argv.** The existing rule is that a secret is not a command argument (`rejectTokenArg`), and a code holds a secret.
- **Minting happens on the lake host through the CLI.** `serve` gains a subcommand that writes a pending registration into the lake's store and prints the code. The web dashboard has only a `viewer` role and its administration is out of scope in `docs/web-dashboard.md`, so a web "mint a code" page is a follow-up, not part of this epic.
- **The lake's base configuration can narrow and suggest, and local config wins.** Allowed fields: `harnesses`, `agent` debounce values, `redaction`, `projects.deny`, and `projects.allow` rules scoped to that lake only. A local `deny` always wins over a lake-supplied `allow`, and a local file can override every field. Rejected: letting a lake widen the allowlist of another lake or the global deny set, because that would let one lake's operator pull sessions meant for another.
- **Each lake gets its own machine id.** A shared id would let whoever reads two lakes join them by machine. The existing `machine.json` becomes the id of the lake migrated from the legacy config, so data already on the hosted lake keeps its provenance. This is the decision most worth the owner's second look, since the tenant today is one person's machines.
- **Per-lake state lives under `StateDir/lakes/<lake-id>/`.** A legacy single-lake state directory migrates into it once, on first start of the new binary, and the old files are left in place until the migration has committed.

### Out of scope

A web page for minting codes or revoking devices, the lake dialing the agent, object storage, multi-tenant lakes, and code delivery by QR or email.

## Acceptance criteria

- [ ] A fresh machine goes from a registration code to syncing with one command and no hand-copied token or config
- [ ] A standalone agent running with no lake starts syncing after a code is entered, without a restart
- [ ] One agent reports to two lakes, each with its own token, allowlist and state, and one lake being down does not stall the other
- [ ] Existing single-lake clients and the hosted lake's token file upgrade in place with no re-registration and no re-upload
- [ ] Registration refuses a code whose key is not listed as active at the code's URL, or whose lake cannot sign a fresh nonce with it

## Definition of done

- [ ] All children of this epic are done
- [ ] docs/policy.md records the reversed enrolment decision with owner sign-off

## Notes

**agent:claude-code/e4a47e8c** at 2026-09-26T19:41:23Z

2026-09-27, owner asked for a published key endpoint. Added /.well-known/terva-lampi/keys with a nonce signature to TKT-01M3FHHBF, the four-step check to TKT-01M3FHHBR, and rotation as TKT-01M3FKS3XHYM1QXR4Q56SGWY6Y. It is a nonce on the key endpoint rather than a hello challenge because an unregistered agent has no token for hello. Supersedes the signature bullet as first filed: the endpoint moves the key binding onto the TLS certificate of the code's URL, but a forged code for an attacker's own URL is still caught only by the fingerprint confirmation.
