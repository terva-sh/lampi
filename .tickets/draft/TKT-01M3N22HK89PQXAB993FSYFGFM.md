---
schema: 3
id: TKT-01M3N22HK89PQXAB993FSYFGFM
title: "Dashboard: Allow can target the device's override, with a note"
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
assignees: []
milestone: null
parent: TKT-01M3N22HCYT06PPNZFQP3YCCCE
origin: null
dependencies:
  - TKT-01M3N22HGYDZCVMK66PNTGF9QM
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-28T22:27:24Z
updated_at: 2026-09-28T22:27:24Z
created_by:
  id: agent:claude-code/2cf53976
  name: ""
updated_by:
  id: agent:claude-code/2cf53976
  name: ""
extensions: {}
---

## Description

Allow on a refused project (TKT-01M3M7M11S) adds a rule to the device's profile today, through the profile editor. With overrides, it offers a choice: the profile, which reaches every device on it, or this device alone.

- **Profile target.** Keeps today's flow, with the optional revision note.
- **Device target.** Opens the override editor with the rule added and previewed. The note is required there, per the owner's decision.
- The same checks hold for either target:
  - the project must still be refused in the device's newest inventory;
  - a deny rule or a missing cwd has no Allow;
  - a rule already covering the project in the chosen layer is reported instead of added.

## Acceptance criteria

- [ ] Allow offers the profile or this device alone
- [ ] Allowing for this device alone requires an operator note
