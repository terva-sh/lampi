---
schema: 3
id: TKT-01M3M7KB32E2BCFEA9CN710522
title: "Lake-managed agent config: dashboard editing, push, inventory"
type: epic
status: done
status_reason: "Every child landed on main through PRs #65-#110; the owner asked to finish the epic and to track overrides in TKT-01M3N22HCY."
priority: high
due_on: null
labels:
  - area/server
  - area/agent
  - area/protocol
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-28T14:44:43Z
updated_at: 2026-09-28T22:27:43Z
created_by:
  id: agent:claude-code/2cf53976
  name: ""
updated_by:
  id: agent:claude-code/2cf53976
  name: ""
extensions: {}
---

## Description

The lake operator manages every agent's configuration from the dashboard: see what each device captures, edit the profiles that decide what it uploads, and have agents pick up a change within seconds instead of within the hour.

Raised on 2026-09-28 after the first remote agent (tehbeast) registered against a lake whose `default` profile had no allow rules and refused all 221 of its sessions. The owner could not see that from the lake, and fixing it meant editing `profiles.json` on the lake host and waiting for the hourly poll.

### Owner decisions (2026-09-28, Drew Short)

- The dashboard can view, add, edit and remove profiles, covering every rule type a profile carries.
- Pushing a change out quickly is the main goal. Agents keep the last verified profile durably and resume from it when they cannot fetch a new one. The atomic cache in `lakes/<name>/profile.json` already does this.
- Agents may report project names, sizes, counts, projects and directory paths to the lake, including projects the allowlist refuses. This mode is called SOCIABLE and is the default. A STRICT mode, set only on the agent, reports nothing about projects outside the allowlist, but still accepts profile updates and still reports what is allowlisted.
- Per-device overrides come later. The data model and the editor are designed now so that a device-level layer can be added without a migration of meaning.

### Design outline

1. Policy text for the inventory and the two modes.
2. Heartbeat: durable last contact, sync counters, applied profile version.
3. Inventory report, gated by the agent's mode.
4. Profiles stored in the catalog with versions and an audit trail, in a layered shape (profile, then a future device overlay).
5. Push: the lake advertises the current profile version, and the agent fetches as soon as it changes.
6. Dashboard profile editor.
7. Dashboard device page: inventory, applied or stale profile, and an "allow this project" action.

The device actions in TKT-01M3J5HXA (revoke, unbind, set profile) sit next to item 7.

## Acceptance criteria

- [x] Operators edit every profile field from the dashboard
- [x] A profile change reaches agents within seconds
- [x] Each device page shows what the agent sees, per its inventory mode

## Notes

**agent:claude-code/2cf53976** at 2026-09-28T15:00:45Z

Owner decisions 2026-09-28: (1) STRICT sends aggregate refused counts with no names, recorded on TKT-01M3M7M0PM. (2) Profiles enter the catalog only through an explicit 'serve profiles import', and a leftover profiles.json is ignored loudly, recorded on TKT-01M3M7M0WC. (3) Per-device overrides, when designed, carry an operator note so the operator remembers why they chose it. Profile revisions get the same optional note now, so the device layer can reuse it.

**agent:claude-code/2cf53976** at 2026-09-28T22:13:32Z

Every child is done once #106-#110 merge: the policy, heartbeat, catalog profiles, push, profile editor, inventory report (TKT-01M3M7M0TH) and device page with Allow (TKT-01M3M7M11S). The first three criteria are met. The fourth, the operator note on per-device overrides, waits on the overrides themselves, which the owner deferred and which no ticket tracks yet. Whether to close this epic with that criterion open, or to file the overrides as a new ticket and move the criterion there, is the owner's call.

**agent:claude-code/2cf53976** at 2026-09-28T22:27:33Z

The fourth criterion, the operator note on per-device overrides, moved to TKT-01M3N22HCY (Per-device overrides on top of agent profiles), filed at the owner's request with children for the catalog layer, the dashboard editor and Allow's device target. Every remaining criterion here is met.

**agent:claude-code/2cf53976** at 2026-09-28T22:27:43Z

draft to done: Every child landed on main through PRs #65-#110; the owner asked to finish the epic and to track overrides in TKT-01M3N22HCY.

## Summary

Done. Operators view, add, edit, roll back and remove profiles from the dashboard. Agents fetch a changed profile within seconds of the lake naming its version. Each device has a page with its status, profile state, config sources and inventory, gated by its SOCIABLE or STRICT mode, plus an Allow action for refused projects. Per-device overrides continue in TKT-01M3N22HCY (Per-device overrides on top of agent profiles).
