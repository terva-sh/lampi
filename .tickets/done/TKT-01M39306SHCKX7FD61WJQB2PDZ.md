---
schema: 3
id: TKT-01M39306SHCKX7FD61WJQB2PDZ
title: "Config: harnesses schema"
type: task
status: done
status_reason: null
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
updated_at: 2026-09-24T07:47:59Z
created_by:
  id: agent:atlas/architect
  name: Atlas - Architect
updated_by:
  id: agent:cursor/eee6
  name: Cursor cloud agent
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

- [x] `File` (or equivalent) parses optional `harnesses` map with `enabled` and `root` only
- [x] Unknown harness key fails config load with a clear error naming the key
- [x] Relative `root` fails config load
- [x] Missing `harnesses` unmarshals to the same effective behavior as today
- [x] Unit tests cover omit, unknown key, relative root, and absolute root round-trip
- [x] No allowlist or secret fields are accepted on harness entries

## Definition of done

- [x] Schema and tests land under `internal/config` (or the existing client config package)
- [x] `go test` for that package is green
- [x] No agent discover/watch behavior change in this ticket (that is TKT-01M39306SKH81TGQ69HSV578FF)

## Implementation plan

### Approach

Add an optional Harnesses map on config.File. LoadFile stays on json.Unmarshal, so unknown top-level keys are still dropped. A harness entry decodes with DisallowUnknownFields, so allowlist, secret, token, and any other key is a load error.

Keys must be the protocol harness ids: terva, claude, codex, opencode, cursor, cursor-cli. Any other key fails the load and the error quotes that key.

enabled omitted stores Enabled true. enabled false is marshaled explicitly so it round-trips. root omitted stores no path. An empty root is a load error, not an omit. A relative root is a load error. An absolute root is stored as written. The path does not have to exist. filepath.IsAbs decides absolute, including on Windows.

A missing harnesses map, an empty map, or a missing key leaves no entry for that harness. Harnesses.Enabled reports that case as on, which is today's behavior. Projects and Redaction are unchanged. Agent discover and watch do not read the map.

Root precedence is a comment on HarnessConfig, not a resolver. The contract is flag, if a flag exists, then config root, then env, then the adapter default. No flag exists today. This change does not read env and does not add a flag.

### Tests

internal/config covers omit and empty map, a partial map, projects and redaction beside harnesses, each known key, unknown spellings, unknown entry fields, enabled omit and false, empty and relative root, and absolute root round-trip.

## Summary

config.File accepts an optional harnesses map. Keys are the protocol harness ids terva, claude, codex, opencode, cursor, and cursor-cli. An unknown key fails LoadFile and the error quotes the key.

Each entry accepts enabled and root only. Unknown fields, including allowlist, allow, deny, secret, token, api_key, and path, fail the load. The rest of the file still drops unknown top-level keys. enabled omitted stores Enabled true. enabled false round-trips. A missing root stores no path. An empty or relative root fails the load. An absolute root is stored as written, does not have to exist, and round-trips. filepath.IsAbs is the absolute check.

A missing map, an empty map, or a missing key leaves no entry. Harnesses.Enabled reports that case as on, which is today's behavior. Projects and redaction still load beside the map. A missing config.json is still an empty File.

Root precedence is documented on HarnessConfig and is not resolved. The contract is flag, if a flag exists, then config root, then env, then the adapter default. No flag exists. Discover, watch, and upload do not read the map. That wiring is TKT-01M39306SKH81TGQ69HSV578FF (Agent: honor harness enable + root).

go test ./internal/config/ -count=1 passed.
