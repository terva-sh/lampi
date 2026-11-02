---
schema: 3
id: TKT-01M6F7JMWA46XCWJGV4VPZCFED
title: "Read token form: bay checkboxes, session scope from a pick"
type: task
status: in-progress
status_reason: null
priority: low
due_on: null
labels:
  - area/server
  - area/auth
assignees: []
milestone: null
parent: TKT-01M6F7JMD2S5S9Q418SYSYDM34
origin: null
dependencies:
  - TKT-01M6F7JMQYGY3WGGV55H0M3FR5
blocks_on: none
references: []
claim:
  actor: agent:claude-code/t3-43103b81
  branch: t3/read-token-pickers
  worktree: /home/sothr/.t3/worktrees/lampi/t3-43103b81
  commit: 82ba348836178a700b7fd890e3e45e9b53573f37
  session: null
  claimed_at: 2026-11-02T23:53:45Z
  expires_at: null
archive: null
created_at: 2026-11-02T22:55:13Z
updated_at: 2026-11-02T23:57:30Z
created_by:
  id: agent:claude-code/t3-43103b81
  name: ""
updated_by:
  id: agent:claude-code/t3-43103b81
  name: ""
extensions: {}
---

## Description

Step 3 of the epic. The read-token form takes session UIDs in a textarea and bays as free text, while the registration form already offers bays as checkboxes.

- **Bays:** offer them as checkboxes, by name.
- **Sessions:** keep the explicit UID list as an advanced field. Add a way to build that list from a project or machine pick, using the pickers from the sessions-pickers ticket; the token still stores session UIDs and bays.

A token scoped by project would need a new token scope kind. That is out of scope here unless the owner asks.

## Acceptance criteria

- [x] Bays are checkboxes by name
- [x] The session list can be filled from a project or machine pick, and stays editable

## Implementation plan

Bays as checkboxes (name=bay, the bay names), joined with the older free-text bays field, which still works. A Sessions fieldset holds a Project box and Machine select with the Sessions suggestions (scope AllBays, as the admin guard sets no read scope), a List matching sessions button (fill=1) that fills Session UIDs with up to 100 newest matches through DashboardSessions and mints nothing, and the UID textarea. No match leaves the list unchanged, since empty means every session; more than 100 says to use a bay. A hidden default Mint token button comes first, so Enter still mints; Enter in Project presses List matching sessions through a generalized data-enter hook in lake.js.

## Notes

**agent:claude-code/t3-43103b81** at 2026-11-02T23:57:30Z

TestReadTokenFormPickers covers the checkboxes, the suggestions, a fill (no token minted), a no-match fill that keeps a typed list, a fill with no pick (400), and a mint with a ticked bay. Screenshot after a fill checked. GOFLAGS=-mod=mod just ci passes.
