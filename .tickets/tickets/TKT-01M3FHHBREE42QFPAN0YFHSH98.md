---
schema: 3
id: TKT-01M3FHHBREE42QFPAN0YFHSH98
title: "CLI: terva-lampi register and lakes list/remove for both paths"
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/agent
  - area/auth
assignees: []
milestone: null
parent: TKT-01M3FHHBCJ12FXKNTB6Z138F6N
origin: null
dependencies:
  - TKT-01M3FHHBJAZ54CXQ7J1Y88XGVF
  - TKT-01M3FHHBKTMJCHCMQQQJZKQTMF
  - TKT-01M3FP115FH6DV3V5WGN3XHPJK
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

Part of the agent onboarding epic. Add the client commands for both onboarding paths.

- `terva-lampi register` reads a code from stdin, from an interactive prompt, or from `--code-file`, never from argv. Before it redeems the code it checks, in order:
  1. The code's signature verifies against the key it carries, and the code has not expired. The client allows five minutes of clock skew and, past that, names the local clock as a likely cause. The lake remains the authority on expiry.
  2. The URL is `https://`, or `http://` on loopback, which is the rule the client already applies.
  3. `GET /.well-known/terva-lampi/keys` at that URL, with a fresh random nonce, lists the code's key as active under the code's lake id, and its signature over the nonce verifies against that key.
  4. The operator confirms the URL, lake id and key fingerprint. Interactive use prompts. `--fingerprint SHA` checks non-interactively against the value `serve identity` prints. A run with no terminal and no `--fingerprint` refuses.

  Any failure refuses the registration and names the check that failed. Then it generates the device token, redeems the code, writes the token file at 0600 and the lake entry into `config.json`, stores the verified base configuration, and signals a running agent to reload. On Windows it says the agent must be restarted instead. It never prints the token.
- **Path 1, a fresh machine:** `terva-lampi register` with no existing config creates the config directory, the state directory and the lake entry. `--install-service` optionally writes and enables the systemd user unit or the launchd agent from `deploy/`, and suggests `loginctl enable-linger` on Linux when the user has no lingering session.
- **Path 2, a standalone agent:** the agent is already running with no lake, or with other lakes. `register` adds one lake and the running agent picks it up.
- `terva-lampi lakes list` and `terva-lampi lakes remove NAME` manage the set. `remove` keeps that lake's state directory unless `--purge-state` is given.
- A new client that meets a lake with no key endpoint refuses with a message that the lake must be upgraded first.
- A second `register` for a lake already present, matched by lake id, refuses and names the existing entry. Re-registering takes an explicit `--replace`.

## Acceptance criteria

- [ ] register reads the code from stdin, a prompt or a file, never from argv, and never prints the token
- [ ] register refuses a tampered or expired code, a non-loopback http URL, or a lake whose hello key differs from the code
- [ ] On a fresh machine register creates everything needed to sync, and --install-service enables the user unit
- [ ] Against a running agent, register adds the lake live, and a duplicate lake id needs --replace
- [ ] lakes list and lakes remove work, and remove keeps state unless --purge-state is given
- [ ] register refuses when the published key list at the code's URL does not list the code's key as active or its nonce signature fails
- [ ] register shows the URL, lake id and fingerprint and needs confirmation, or --fingerprint when there is no terminal
