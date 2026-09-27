---
schema: 3
id: TKT-01M3J5HX8GGBVSVVHX82W60VJE
title: "Installer: register from a code in the environment with a fingerprint"
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/agent
  - area/ops
  - policy
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-27T19:30:30Z
updated_at: 2026-09-27T20:52:53Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/e4a47e8c
  name: ""
extensions: {}
---

## Description

The dashboard's registration page (see the ticket that depends on this one) hands the operator one line that installs and registers a machine with nothing to type. `install.sh --register` today asks for the code and the fingerprint on the terminal, so the line needs two additions.

### Approach

- **`TERVA_LAMPI_CODE`**: when set with `--register`, install.sh passes the code to `terva-lampi register` on stdin (printf into a pipe). It never goes into argv, so `ps` cannot show it. install.sh then unsets it.
- **`--fingerprint SHA256:...`**: passed through to register. It replaces the confirmation prompt, so the line also works as `ssh host '...'` with no terminal. The no-terminal refusal applies only when neither the code nor a fingerprint is given.
- `register` already reads its code from a non-terminal stdin and already takes `--fingerprint`, so register itself does not change.
- Update `docs/policy.md`, which says the code is read "from stdin, a prompt, or a file, and never from a command argument". An environment variable on the installer's line is none of those. Record the trade-off (below) and the owner's sign-off.

### The trade-off

A one-liner that carries the code leaves it in shell history until it is spent, and a copy read from history before redemption could register a different machine. Mitigations:

- The line starts with a space (bash `ignorespace`, zsh `HIST_IGNORE_SPACE`).
- The dashboard mints one-liner codes with a 1 hour expiry.
- A code redeems once.
- register verifies the lake key before redeeming.

Alternative kept: a one-liner without the code that prompts for it. The dashboard offers it as the second option.

## Acceptance criteria

- [ ] install.sh --register reads TERVA_LAMPI_CODE and hands it to register on stdin, never argv
- [ ] --fingerprint passes through so the one-liner works without a terminal
- [ ] Tests cover env code, fingerprint, and the existing prompt path
- [ ] docs/policy.md records the env-var entry and the owner's sign-off

## Notes

**agent:claude-code/e4a47e8c** at 2026-09-27T20:47:26Z

Owner sign-off, 2026-09-27: the copyable one-liner may carry the registration code. The owner's reasoning: a code is minted for one machine and redeems once, so once it is used, the copy left in shell history is only noise. docs/policy.md should record this when the ticket lands.
