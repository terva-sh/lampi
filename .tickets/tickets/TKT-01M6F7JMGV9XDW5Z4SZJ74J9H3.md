---
schema: 3
id: TKT-01M6F7JMGV9XDW5Z4SZJ74J9H3
title: "Dashboard: wide list pages, wrapping project cells, machine names"
type: task
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
assignees: []
milestone: null
parent: TKT-01M6F7JMD2S5S9Q418SYSYDM34
origin: null
dependencies: []
blocks_on: none
references: []
claim:
  actor: agent:claude-code/t3-43103b81
  branch: t3/wide-layout
  worktree: /home/sothr/.t3/worktrees/lampi/t3-43103b81
  commit: 05b9765e4514c17fcc3425e1138563085a56fee1
  session: null
  claimed_at: 2026-11-02T23:06:10Z
  expires_at: null
archive: null
created_at: 2026-11-02T22:55:13Z
updated_at: 2026-11-02T23:10:33Z
created_by:
  id: agent:claude-code/t3-43103b81
  name: ""
updated_by:
  id: agent:claude-code/t3-43103b81
  name: ""
extensions: {}
---

## Description

Step 1 of the epic: the wide layout with no ellipsis.

- **Width:** list and table pages use the wide main, up to 1600px. Prose pages and forms stay readable.
- **No ellipsis:** the project, path and session-name cells wrap at `/` and `-` instead of being cut off; drop the fixed 22% first column where it starves the content.
- **Machine names:** show a device's name where a machine ULID is shown today: the sessions Machines column, provenance and operations. The `machineName` helper and the conflict pages' device map show the way. Revoked and unbound machines fall back to the short ID.
- **Times:** show them as `2006-01-02 15:04:05Z`, with the full value in the `datetime` and `title` attributes.

### Acceptance check

Screenshot each changed view at 1500px and at a narrow width.

## Acceptance criteria

- [ ] List and table pages use the wide main; project, path and session cells wrap instead of eliding
- [ ] Machines show by device name, falling back to the short ID
- [ ] Times show as readable UTC with the full value in datetime and title
- [ ] Screenshots at wide and narrow widths checked; tests pin the markup

## Implementation plan

Template funcs: when (RFC 3339 to 'YYYY-MM-DD HH:MM:SSZ' UTC, else as given), wrapPath (escape, <wbr> after / and :), wideView (views whose main is wide), named (hands sessions/records tables their rows plus device names). All 27 plain <time> elements get title= and when. pageData.MachineNames from Server.machineNames (factored out of newConflictsView) on overview, sessions, detail, conflicts; sessions Machines column and provenance show machineName with the ID on hover. CSS: session-name and project-label wrap (overflow-wrap:anywhere) instead of ellipsis; sessions-table column widths; harness badge nowrap. Wide views: overview, sessions, detail, search, activity, usage, operations, devices, device, review, profiles, profile, profile-edit, bays, conflicts, registrations, read-tokens, minted, raw. Narrow: transcript, refused, review-allow, conflict.

## Notes

**agent:claude-code/t3-43103b81** at 2026-11-02T23:10:33Z

Screenshots at 1800px and 900px of /sessions with long remotes and folders (scratch wide.*): projects wrap at separators, machines show 'brokkr', times read as 2026-11-02 23:14:10Z. At 900px the harness badge wrapped mid-word with 8%; widened to 10%. GOFLAGS=-mod=mod just ci passes.
