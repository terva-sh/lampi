---
schema: 3
id: TKT-01M3M7M0ZY7A4NFTEHTDTND9KF
title: "Dashboard: view, add, edit and remove agent profiles"
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area/server
  - area/auth
assignees: []
milestone: null
parent: TKT-01M3M7KB32E2BCFEA9CN710522
origin: null
dependencies:
  - TKT-01M3M7M0WCZQB2ETXX1PNKHRBY
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-28T14:45:05Z
updated_at: 2026-09-28T14:45:06Z
created_by:
  id: agent:claude-code/2cf53976
  name: ""
updated_by:
  id: agent:claude-code/2cf53976
  name: ""
extensions: {}
---

## Description

Operators view, add, edit and remove profiles from the dashboard, covering every field a profile can carry.

- List: name, version, updated at and by, and the devices that use it.
- Editor fields:
  - `projects.allow` and `projects.deny` rules with every field of `ProjectMatch` (`cwd_prefix`, `cwd_hash`, `git_remote`, `git_remote_prefix`). A rule may combine fields, and every field set must match.
  - Harness enable toggles.
  - `agent.debounce` and `agent.debounce_max`.
  - Anything else `Profile` allows at the time. Fields the agent refuses (`harnesses.*.root`, `redaction.upload_hits`) are not offered.
- Show `git_remote` in the normalized form `NormalizeRemote` produces, and normalize input on save, so a rule typed as `git@host:org/repo` matches.
- Before saving, show the diff and the devices it will reach. For devices that report `allow_source=local`, warn that the change will not affect their allow list.
- Operator role only; CSRF-checked POSTs; the audit log carries the OIDC actor and the diff. Viewers see profiles read-only.
- Rolling back to an earlier revision is a single action.
- Leave room in the layout for a device-override layer (for example a tab), without building it yet.

## Acceptance criteria

- [ ] Operators edit every profile field, including all ProjectMatch fields
- [ ] Saves show a diff and the devices reached, and are audited with the OIDC actor
- [ ] A revision can be rolled back
