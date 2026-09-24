---
schema: 3
id: TKT-01M39306SKH81TGQ69HSV578FF
title: "Agent: honor harness enable + root"
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

Wire `terva-lampi agent` / sync discover, watch, and upload to Shape A config.

For each known harness, resolve root with precedence flag > config `root` > env > adapter default. If `enabled` is false, skip discover, watch, and upload for that harness only. Do not delete or rewrite watermarks. Do not touch CAS objects.

## Acceptance criteria

- [ ] Disabled harness is absent from discover/watch/upload paths; other harnesses unchanged
- [ ] Config `root` overrides env and adapter default; env still overrides default when config `root` is omitted
- [ ] Disabling a harness does not clear watermarks or mutate CAS
- [ ] Re-enabling a harness resumes from existing watermarks
- [ ] Tests cover enable=false skip and root precedence (config beats env beats default)

## Definition of done

- [ ] Agent/sync path honors the schema from TKT-01M39306SHCKX7FD61WJQB2PDZ
- [ ] `go test ./...` green for touched packages
- [ ] No VPS, restic, frontend, purge, or soft-link work
