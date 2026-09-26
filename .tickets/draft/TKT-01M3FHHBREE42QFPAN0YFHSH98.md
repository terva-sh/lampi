---
schema: 3
id: TKT-01M3FHHBREE42QFPAN0YFHSH98
title: "CLI: terva-lampi register and lakes list/remove for both paths"
type: task
status: draft
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
  - TKT-01M3FHHBPHPZHBTJT794N8HCZW
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

Part of the agent onboarding epic. Add the client commands for both onboarding paths.

- `terva-lampi register` reads a code from stdin, from an interactive prompt, or from `--code-file`, never from argv. It verifies the signature against the embedded key and refuses an expired code. It refuses a plain `http://` URL that is not loopback, which is the rule the client already applies. It calls `hello` and checks that the lake id and key match the code. It generates the device token, redeems the code, writes the token file at 0600 and the lake entry into `config.json`, stores the verified base configuration, and signals a running agent to reload. It never prints the token.
- **Path 1, a fresh machine:** `terva-lampi register` with no existing config creates the config directory, the state directory and the lake entry. `--install-service` optionally writes and enables the systemd user unit or the launchd agent from `deploy/`, and suggests `loginctl enable-linger` on Linux when the user has no lingering session.
- **Path 2, a standalone agent:** the agent is already running with no lake, or with other lakes. `register` adds one lake and the running agent picks it up.
- `terva-lampi lakes list` and `terva-lampi lakes remove NAME` manage the set. `remove` keeps that lake's state directory unless `--purge-state` is given.
- A second `register` for a lake already present, matched by lake id, refuses and names the existing entry. Re-registering takes an explicit `--replace`.

## Acceptance criteria

- [ ] register reads the code from stdin, a prompt or a file, never from argv, and never prints the token
- [ ] register refuses a tampered or expired code, a non-loopback http URL, or a lake whose hello key differs from the code
- [ ] On a fresh machine register creates everything needed to sync, and --install-service enables the user unit
- [ ] Against a running agent, register adds the lake live, and a duplicate lake id needs --replace
- [ ] lakes list and lakes remove work, and remove keeps state unless --purge-state is given
