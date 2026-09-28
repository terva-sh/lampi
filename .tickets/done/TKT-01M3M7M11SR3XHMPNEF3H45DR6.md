---
schema: 3
id: TKT-01M3M7M11SR3XHMPNEF3H45DR6
title: "Dashboard: per-device page with inventory and allow action"
type: task
status: done
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
claim: null
archive: null
created_at: 2026-09-28T14:45:05Z
updated_at: 2026-09-28T22:13:22Z
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

- [x] Each device shows status, counters, applied vs current profile and config sources
- [x] SOCIABLE devices list their projects; STRICT devices show allowlisted only
- [x] Allow on a refused project adds a rule to the device's profile

## Implementation plan

Two PRs, stacked on the inventory work.

### P1 (web/device-page, #108): the page

- `GET /devices/{id}` and `GET /api/web/v1/devices/{id}`. The row is read through `readDevices`, so the page and the list cannot disagree. The inventory comes from `DeviceInventoryOf`.
- Status, agent against the lake, inventory mode, last contact, advisory, the last sync, profile applied vs current, and allow/deny sources.
- A projects table with a `?show=refused` filter. The filter is ignored for a strict device, which lists no refused project.
- Device actions move onto the page. A hidden `from=device` field brings the redirect and any refusal back to it.
- Devices rows and the Operations Machines table link to the page.

### P2 (web/device-allow, #109): Allow

- `POST /devices/{id}/allow` finds the refused row in the device's newest inventory and builds a `git_remote` rule, else `cwd_prefix`.
- It opens the existing profile editor with the rule added and previewed: the diff, the devices reached, the count with local allow rules, and a prefilled note.
- The operator saves through the editor's save with the revision the preview read (`PutProfileIf`).

### Alternatives considered

- **A separate allow confirmation page with its own save.** Rejected: it would repeat the editor's preview, conflict handling and note, and could drift from them.
- **Trusting the posted remote and cwd.** Rejected in review: the fields prove nothing about the verdict, so the row is looked up.
- **An equality check for "already allowed".** Rejected in review: a covering prefix rule already allows the project, so the check asks `Projects.Permitted`.
- **Targeting the device layer.** Waits for per-device overrides, per the owner.

## Notes

**agent:claude-code/2cf53976** at 2026-09-28T15:00:45Z

Owner 2026-09-28: when per-device overrides land, the Allow action targeting the device layer must ask for an operator note. Until then, the Allow action should offer the optional revision note on the profile save.

## Summary

Landed in #108 (the device page) and #109 (Allow).

- **The page.** `/devices/{id}` and its API show a device's status, counters, applied vs current profile, config sources and newest inventory. A sociable device lists every project and filters to the refused ones. A strict device lists allowlisted projects and a refused count. The actions (set profile, unbind, revoke) are on the page, and Devices and Operations link to it.
- **Allow.** Allow on a refused project checks the row against the device's inventory and adds a `git_remote` or `cwd_prefix` rule to the device's profile. It does that through the profile editor's preview and save, with an optional note. Deny-refused and no-cwd rows have no Allow.
- **Smoke fixture.** The fixture now carries sociable and strict inventories.
- **Docs.** web-dashboard.md and web-api.md.
