---
schema: 3
id: TKT-01M39306SNS6A9VC1RYA8ERTTE
title: "status: resolved harnesses"
type: task
status: ready
status_reason: Queued after normalize epic TKT-01M38RJCDREDTTPTY2D7SR8W59. Depends on harnesses schema.
priority: normal
due_on: null
labels:
  - area/ops
  - area/agent
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

Extend `terva-lampi status` (or the existing operator status surface) so each known harness reports enabled, resolved root, and resolution source (`config`, `env`, or `default`).

## Acceptance criteria

- [ ] Status lists every known harness id with enabled bool, absolute resolved root (or explicit empty/missing), and source
- [ ] Source is `config` when config `root` won, `env` when env won, `default` when adapter default won
- [ ] Disabled harness still shows resolved root/source but enabled=false
- [ ] Output is stable enough for ops docs to cite

## Definition of done

- [ ] Status output and tests land
- [ ] Depends on schema from TKT-01M39306SHCKX7FD61WJQB2PDZ; may share root-resolution helper with TKT-01M39306SKH81TGQ69HSV578FF
