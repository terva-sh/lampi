---
schema: 3
id: TKT-01M39306SHCKX7FD61WJQB2PDZ
title: "Config: harnesses schema"
type: task
status: ready
status_reason: Queued after normalize epic TKT-01M38RJCDREDTTPTY2D7SR8W59. Depends on parent epic queue.
priority: normal
due_on: null
labels:
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

Extend client `config.json` parsing for Shape A optional `harnesses` map.

Each key is a protocol harness id. Each value may set `enabled` (bool) and optional `root` (absolute path string). Do not accept allowlist fields or secret/token fields on a harness entry.

### Semantics (locked)

- Precedence for root resolution (implemented here as the documented contract; agent wiring is the sibling ticket): flag > config `root` > env > adapter default
- Omit `harnesses` or omit a harness key = today's behavior for that harness
- Unknown harness key = error on config load
- `enabled` omitted = enabled (default on)
- Relative `root` = error

## Acceptance criteria

- [ ] `File` (or equivalent) parses optional `harnesses` map with `enabled` and `root` only
- [ ] Unknown harness key fails config load with a clear error naming the key
- [ ] Relative `root` fails config load
- [ ] Missing `harnesses` unmarshals to the same effective behavior as today
- [ ] Unit tests cover omit, unknown key, relative root, and absolute root round-trip
- [ ] No allowlist or secret fields are accepted on harness entries

## Definition of done

- [ ] Schema and tests land under `internal/config` (or the existing client config package)
- [ ] `go test` for that package is green
- [ ] No agent discover/watch behavior change in this ticket (that is TKT-01M39306SKH81TGQ69HSV578FF)
