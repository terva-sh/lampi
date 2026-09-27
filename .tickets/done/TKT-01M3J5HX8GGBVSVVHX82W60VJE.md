---
schema: 3
id: TKT-01M3J5HX8GGBVSVVHX82W60VJE
title: "Installer: register from a code in the environment with a fingerprint"
type: task
status: done
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
updated_at: 2026-09-27T21:05:38Z
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

- [x] install.sh --register reads TERVA_LAMPI_CODE and hands it to register on stdin, never argv
- [x] --fingerprint passes through so the one-liner works without a terminal
- [x] Tests cover env code, fingerprint, and the existing prompt path
- [x] docs/policy.md records the env-var entry and the owner's sign-off

## Implementation plan

install.sh reads `TERVA_LAMPI_CODE` once and unsets it, then takes one of two paths:

- **With `--fingerprint`:** the code goes to `register` through a printf pipe. printf is a builtin, so the code is in no argv, and no terminal is needed.
- **Without it:** the code goes into a mode-0600 file in the installer's private temporary directory, passed with `--code-file`. The terminal answers the confirmation, and the file is removed on exit.

Impossible combinations are refused before any download: a code or fingerprint without `--register`, and a code with neither a fingerprint nor a terminal. register itself does not change.

Deviation from the ticket's wording: the plan said stdin only. The code-file path covers the case with a terminal but no fingerprint, where stdin must stay the terminal for the prompt. A file is one of the entry points the policy already allows.

## Notes

**agent:claude-code/e4a47e8c** at 2026-09-27T20:47:26Z

Owner sign-off, 2026-09-27: the copyable one-liner may carry the registration code. The owner's reasoning: a code is minted for one machine and redeems once, so once it is used, the copy left in shell history is only noise. docs/policy.md should record this when the ticket lands.

**agent:claude-code/e4a47e8c** at 2026-09-27T21:02:41Z

### Evidence

**Tests** (`TestInstallScript*`, with a fake binary recording its arguments, stdin and environment):
- The env code plus a fingerprint, with no terminal: register's argv is `register --install-service --lake work --fingerprint SHA256:abc`, stdin is the code, and it does not inherit `TERVA_LAMPI_CODE`.
- Each impossible combination is refused with zero HTTP requests.

**End to end:** a throwaway lake on loopback, a real code from `serve register --expires 1h`, and the real binary served as a fake release. The installer ran under setsid with stdin /dev/null and `systemctl` stubbed.
- Installer exit 0.
- The code appeared 0 times in the output.
- `serve devices list` showed e2e-box active and bound to its machine id, and the code showed as used.
- The unit was written and the stub got `enable --now`.

**Pseudo-terminal path** (code in the env, no fingerprint): register got `--code-file` on a mode-600 file holding the code, with stdin on the terminal and no inherited env. The file was gone after exit.

**Docs:** policy.md records the owner's sign-off, and registration-and-lakes.md shows the one-liner.

## Summary

The installer takes the code from TERVA_LAMPI_CODE and the lake key from --fingerprint, so one copied line installs and registers a machine with no terminal. The code reaches register on a pipe, or in a private 0600 file when the terminal confirms instead. It is never an argument and is not inherited by the agent. An empty --fingerprint or --lake is refused (review f77ad362 finding-1). Verified end to end against a loopback lake with a real code. docs/policy.md records the owner's sign-off. Landed in PR #31.
