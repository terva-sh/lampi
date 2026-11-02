---
schema: 3
id: TKT-01M6EJ43MTF221EJQAQH7K7QT7
title: "Dashboard: profile Health panel with safe fixes and remove-and-hide"
type: task
status: ready
status_reason: null
priority: high
due_on: null
labels:
  - policy
  - area/server
assignees: []
milestone: null
parent: TKT-01M3NM01J4731BSWEA815FWGXS
origin: null
dependencies:
  - TKT-01M6EJ43HT4EZH4Y51221C3CGE
blocks_on: none
references: []
claim:
  actor: agent:claude-code/t3-43103b81
  branch: t3/profile-doctor
  worktree: /home/sothr/.t3/worktrees/lampi/t3-43103b81
  commit: 066b521a6ffed031e1bf13f8a112c4a5e61d8981
  session: null
  claimed_at: 2026-11-02T16:40:25Z
  expires_at: null
archive: null
created_at: 2026-11-02T16:40:17Z
updated_at: 2026-11-02T16:40:25Z
created_by:
  id: agent:claude-code/t3-43103b81
  name: ""
updated_by:
  id: agent:claude-code/t3-43103b81
  name: ""
extensions: {}
---

## Description

Show the profile doctor's report from TKT-01M6EJ43HT4EZH4Y51221C3CGE (Profile doctor: rule checks and serve profiles doctor) on the dashboard, and offer its fixes in the profile editor.

- **A Health panel** on the profile page and in the editor. It replaces the editor's "Fewer rules" panel, and keeps that panel's offers as findings.
- **One button applies every safe fix** into the form and previews it.
- **One button per narrowing finding.** It replaces a home, top-level or host-wide rule with the exact rules it admits today, or removes a temporary rule. The preview lists what changes.
- **Remove and hide.** A temporary rule a device still holds is removed, and its projects are hidden from review when the operator saves, with a note. The preview names the projects to be hidden. The hide runs after the save succeeds.
- **One button removes the unmatched rules**, which are not safe.

## Acceptance criteria

- [ ] The profile page and the editor show the doctor's findings
- [ ] Apply safe fixes changes the form and previews it, and saves nothing
- [ ] A narrowing fix previews its replacement; a temporary rule's fix hides its projects after a successful save
- [ ] docs/web-dashboard.md describes the panel
