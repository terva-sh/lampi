---
schema: 3
id: TKT-01M3GAHSQ04KE32NFHNH3CRTD9
title: "CLI register: install the systemd unit where the user manager looks"
type: bug
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/agent
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-27T02:19:20Z
updated_at: 2026-09-27T21:35:50Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/e4a47e8c
  name: ""
extensions: {}
---

## Description

Finding-1 from terva-review 994 on PR #18 (TKT-01M3FHHBR, CLI register), deferred at the owner's direction on 2026-09-27 so the onboarding stack could merge. The round-4 note on that ticket already names it as an owner call.

`installSystemd` in internal/cli/service.go writes the unit under `$XDG_CONFIG_HOME/systemd/user/`, then runs `systemctl --user daemon-reload` and `enable [--now] terva-lampi-agent.service`. When `XDG_CONFIG_HOME` is custom for the register process but not for the already-running systemd user manager, the manager does not search that directory. The enable then fails: registration succeeds, but `--install-service` starts no agent.

Options:
- Always install into `~/.config/systemd/user/`, the manager's default path, and keep the custom XDG values in the unit's Environment= lines (added in round 4).
- Enable by file path (`systemctl --user link` / `enable PATH`).
- Detect the mismatch and refuse with a clear message.

The first option looks simplest. Test that with a custom XDG_CONFIG_HOME the unit lands in the default path and still carries the Environment= lines.

## Notes

**agent:claude-code/e4a47e8c** at 2026-09-27T21:30:29Z

### Decision

I departed from the ticket's first option. Hard-coding `~/.config/systemd/user` is still wrong when the user manager itself runs with a custom `XDG_CONFIG_HOME`. So `systemdUserDir` asks the manager with `systemctl --user show-environment`:

- If the manager reports an absolute `XDG_CONFIG_HOME`, the unit goes in `$XDG_CONFIG_HOME/systemd/user`.
- Otherwise it goes in the manager's `$HOME/.config/systemd/user`.
- If the manager cannot be asked (no bus), it falls back to this process's `HOME`, and `enable` reports any problem.

The unit's `Environment=` lines still carry register's custom XDG directories to the agent.

Rejected:

- **Enabling by path** (`systemctl --user link`/`enable PATH`): the unit would live in a directory the manager does not own, and a removed temp directory would break the symlink.
- **Refusing on a mismatch:** it gives the user work the tool can do itself.

### Evidence

- Tests: the register install writes under the manager's `~/.config`, not register's `XDG_CONFIG_HOME`, and still carries `Environment="XDG_CONFIG_HOME=..."`. The call sequence starts with `show-environment`.
- `systemdUserDir` cases: the manager's own config dir, the default, a relative value ignored, and an unreachable manager.
- On this host, `show-environment` reports `HOME` and no `XDG_CONFIG_HOME`, so units go to `~/.config/systemd/user`.

**agent:claude-code/e4a47e8c** at 2026-09-27T21:35:50Z

Supersedes the Decision note above, after terva-review beceb924 finding-1. show-environment is the environment for launched services, and set-environment can change it after the manager built its unit search path. The directory now comes from the manager's UnitPath property (systemctl --user show -p UnitPath --value): the first absolute entry ending in systemd/user and not under /run, /etc or /usr. On this host that is ~/.config/systemd/user. The fallback is HOME/.config/systemd/user when the property cannot be read, or when UnitPath has no such entry, which includes a directory with a space, since systemd separates entries with spaces. This also removes finding-2, the whitespace trimming of show-environment values.

## Summary

register --install-service writes the systemd unit into the user directory of the running manager's unit search path (UnitPath: the systemd/user entry outside /run, /etc and /usr), falling back to ~/.config/systemd/user, instead of under register's own XDG_CONFIG_HOME. The unit keeps Environment= lines for custom XDG directories.
