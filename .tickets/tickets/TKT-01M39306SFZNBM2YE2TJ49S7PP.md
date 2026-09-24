---
schema: 3
id: TKT-01M39306SFZNBM2YE2TJ49S7PP
title: Config/DX Shape A — harness enable and root overrides
type: epic
status: ready
status_reason: Queued after normalize epic TKT-01M38RJCDREDTTPTY2D7SR8W59. Ready but not startable until that epic closes.
priority: normal
due_on: null
labels:
  - area/ops
  - area/docs
assignees: []
milestone: null
parent: null
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

Operator config/DX for harness capture after the normalize epic. Drew locked Shape A: an optional `harnesses` map in client `config.json` with per-harness `enabled` and optional absolute `root`. Env stays a debug override. Project allowlist and secrets stay out of harness blocks.

This epic does not implement normalize projectors, VPS bring-up, restic, AgentsView/deja-vu, enrolment, purge tooling, or soft-link wiring.

### Locked decisions

- Precedence for harness root: flag (if any) > config `root` > env > adapter default
- Missing or omitted `harnesses` key = today's behavior (all wired harnesses use current Home()/env resolution)
- Unknown harness key in `harnesses` = config load error
- `enabled: false` skips discover, watch, and upload for that harness only; watermarks and CAS are untouched
- No allowlist rules and no secrets inside harness blocks
- Restart-to-reload is OK for v1 (same as server/token/allowlist today)

### Known harness keys (must match protocol harness ids)

`terva`, `claude`, `codex`, `opencode`, `cursor`, `cursor-cli`

### Queue

Blocked by normalize epic TKT-01M38RJCDREDTTPTY2D7SR8W59 (Cursor IDE then Cursor CLI children). Do not start children until that epic is done.

## Acceptance criteria

- [ ] Client config accepts optional `harnesses` map per Shape A with the locked precedence and omit/unknown/enable semantics above
- [ ] Agent discover/watch/upload honor enable and root; watermarks/CAS unchanged when a harness is disabled
- [ ] `status` reports per harness: enabled, resolved root, source (`config` | `env` | `default`)
- [ ] Docs/status cover allowlist DX for Cursor global empty-cwd refuse and Cursor CLI missing cwd (no allowlist policy change)
- [ ] Deploy examples show Shape A `harnesses` without introducing VPS/restic/frontend scope
- [ ] Out of scope remains out: VPS cutover, restic, AgentsView/deja-vu, enrolment API, purge CLI, soft-link

## Definition of done

- [ ] All children of this epic are done
- [ ] TKT-01M39306SHCKX7FD61WJQB2PDZ Config: harnesses schema
- [ ] TKT-01M39306SKH81TGQ69HSV578FF Agent: honor enable + root
- [ ] TKT-01M39306SNS6A9VC1RYA8ERTTE status: resolved harnesses
- [ ] TKT-01M39306SQYDYS7Y6X1KXRJXM0 Allowlist DX notes
- [ ] TKT-01M39306SSJQ9WWGHFWEA840FF Deploy examples for harnesses
