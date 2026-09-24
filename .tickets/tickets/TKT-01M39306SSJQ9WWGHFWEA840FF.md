---
schema: 3
id: TKT-01M39306SSJQ9WWGHFWEA840FF
title: Deploy examples for Shape A harnesses
type: task
status: ready
status_reason: Queued after normalize epic TKT-01M38RJCDREDTTPTY2D7SR8W59. Depends on harnesses schema.
priority: normal
due_on: null
labels:
  - area/ops
  - area/docs
assignees: []
milestone: null
parent: TKT-01M39306SFZNBM2YE2TJ49S7PP
origin: null
dependencies:
  - TKT-01M38RJCDREDTTPTY2D7SR8W59
  - TKT-01M39306SHCKX7FD61WJQB2PDZ
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

Update deploy examples / config samples so operators can copy Shape A `harnesses` enable and root overrides. Keep loopback server placeholders. Do not add VPS hostname, restic, or frontend install steps.

## Acceptance criteria

- [ ] An example `config.json` (or documented fragment) shows `harnesses` with at least one `enabled: false` and one `root` override
- [ ] Agent unit/env examples note restart-to-reload and that env remains a debug override under the locked precedence
- [ ] Examples still default to loopback lake URL and do not commit production hostnames or tokens
- [ ] No restic, VPS bring-up, purge, or soft-link content is introduced

## Definition of done

- [ ] Deploy/docs examples updated and referenced from the epic
- [ ] Matches schema from TKT-01M39306SHCKX7FD61WJQB2PDZ
