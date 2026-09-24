---
schema: 3
id: TKT-01M39306SQYDYS7Y6X1KXRJXM0
title: Allowlist DX notes for Cursor cwd refuses
type: task
status: ready
status_reason: Queued after normalize epic TKT-01M38RJCDREDTTPTY2D7SR8W59.
priority: normal
due_on: null
labels:
  - area/docs
  - area/ops
assignees: []
milestone: null
parent: TKT-01M39306SFZNBM2YE2TJ49S7PP
origin: null
dependencies:
  - TKT-01M38RJCDREDTTPTY2D7SR8W59
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-24T06:53:15Z
updated_at: 2026-09-24T06:53:15Z
created_by:
  id: agent:atlas/architect
  name: Atlas - Architect
updated_by:
  id: agent:atlas/architect
  name: Atlas - Architect
extensions: {}
---

## Description

Document (and optionally surface in status) why Cursor IDE global state and Cursor CLI exports without an absolute cwd are refused by the existing default-deny allowlist. No allowlist policy change.

## Acceptance criteria

- [ ] Docs state that Cursor IDE global DB has empty cwd and is refused by design; workspace DB cwd comes from workspace.json
- [ ] Docs state that Cursor CLI needs absolute cwd in sibling meta (or equivalent) or the export is refused
- [ ] Optional: status or sync stderr hint points operators at those cases without changing permit rules
- [ ] `projects` allow/deny schema and default-deny behavior are unchanged

## Definition of done

- [ ] Docs (and optional status hint) merged
- [ ] No allowlist rule engine changes beyond messaging
