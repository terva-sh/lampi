---
schema: 3
id: TKT-01M3M7M11SR3XHMPNEF3H45DR6
title: "Dashboard: per-device page with inventory and allow action"
type: task
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
assignees: []
milestone: null
parent: TKT-01M3M7KB32E2BCFEA9CN710522
origin: null
dependencies:
  - TKT-01M3M7M0TH9PYS59DADPC604P3
  - TKT-01M3M7M0ZY7A4NFTEHTDTND9KF
  - TKT-01M3J5HXAJBA1VR4W4B5H4NB6G
blocks_on: none
references: []
claim:
  actor: agent:claude-code/2cf53976
  branch: config/inventory-agent
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-2cf53976
  commit: 8eb8b7e6a3728ae21fa112d079a5deb7e4870961
  session: null
  claimed_at: 2026-09-28T21:45:35Z
  expires_at: null
archive: null
created_at: 2026-09-28T14:45:05Z
updated_at: 2026-09-28T21:45:35Z
created_by:
  id: agent:claude-code/2cf53976
  name: ""
updated_by:
  id: agent:claude-code/2cf53976
  name: ""
extensions: {}
---

## Description

A page per device, `/devices/{id}`, showing what the agent sees and how it is configured.

- Header: name, id, machine id, state, agent version, inventory mode, last seen, and the last sync counters.
- Configuration: the profile it uses, the applied version against the current one (stale or current), and where the effective config came from (`allow_source`, `deny_source`). A link goes to the profile editor.
- Inventory (SOCIABLE devices): each project with sessions, size, newest session, and allowed or refused with the reason. Filter by refused.
- For a refused project, "Allow" adds a rule to the device's profile: a `git_remote` rule when the remote is known, otherwise a `cwd_prefix` rule. Show the rule and the other devices the profile reaches before saving. Once device overrides exist, the same action can target the device layer instead.
- STRICT devices show allowlisted projects and the refused count only, with a note that the device reports nothing more.
- The Machines table on `/operations` links to this page. The actions from TKT-01M3J5HXA (revoke, unbind, set profile) live here.

## Acceptance criteria

- [ ] Each device shows status, counters, applied vs current profile and config sources
- [ ] SOCIABLE devices list their projects; STRICT devices show allowlisted only
- [ ] Allow on a refused project adds a rule to the device's profile

## Notes

**agent:claude-code/2cf53976** at 2026-09-28T15:00:45Z

Owner 2026-09-28: when per-device overrides land, the Allow action targeting the device layer must ask for an operator note. Until then, the Allow action should offer the optional revision note on the profile save.
