---
schema: 3
id: TKT-01M3GAHSQ04KE32NFHNH3CRTD9
title: "CLI register: install the systemd unit where the user manager looks"
type: bug
status: draft
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
updated_at: 2026-09-27T02:19:20Z
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
