---
schema: 3
id: TKT-01M39306SNS6A9VC1RYA8ERTTE
title: "status: resolved harnesses"
type: task
status: done
status_reason: null
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
updated_at: 2026-09-24T08:20:02Z
created_by:
  id: agent:atlas/architect
  name: Atlas - Architect
updated_by:
  id: agent:cursor/348f
  name: Cursor cloud agent
extensions: {}
---

## Description

Extend `terva-lampi status` (or the existing operator status surface) so each known harness reports enabled, resolved root, and resolution source (`config`, `env`, or `default`).

## Acceptance criteria

- [x] Status lists every known harness id with enabled bool, absolute resolved root (or explicit empty/missing), and source
- [x] Source is `config` when config `root` won, `env` when env won, `default` when adapter default won
- [x] Disabled harness still shows resolved root/source but enabled=false
- [x] Output is stable enough for ops docs to cite

## Definition of done

- [x] Status output and tests land
- [x] Depends on schema from TKT-01M39306SHCKX7FD61WJQB2PDZ; may share root-resolution helper with TKT-01M39306SKH81TGQ69HSV578FF

## Implementation plan

### Where

harnessStatuses in internal/cli/peers.go walks knownSources and calls resolveHome with an empty flag. sources() still drops a skip. terva-lampi status prints one line per row in that order and stops printing the homeLabel lines. sessions still come from countSources on the enabled set.

### Source

config when the entry root is non-empty. env when the harness override is set: TERVA_HOME or ZOT_HOME, CLAUDE_CONFIG_DIR, CODEX_HOME, XDG_DATA_HOME, CURSOR_CONFIG_DIR. default otherwise. Cursor has no override; XDG_CONFIG_HOME and APPDATA stay platform defaults. A Home error leaves root empty and keeps the tier.

### Callers

countSources takes the harness map the caller already loaded. loadAgent and status pass file.Harnesses instead of configuredSources loading the file again.

## Summary

terva-lampi status prints one line per known harness, in knownSources order. The spelling is locked in the command help and in tests:

harness <id> enabled=<true|false> root=<absolute path or empty> source=<config|env|default>

### Resolution

harnessStatuses calls resolveHome with an empty flag. sources() still omits a disabled harness, so discover, watch, and upload are unchanged. A disabled harness stays on the status line with enabled=false and the same root and source. root is empty when Home fails. source is config when the config root is set, env when the harness override is set, and default otherwise.

The override is TERVA_HOME or ZOT_HOME, CLAUDE_CONFIG_DIR, CODEX_HOME, XDG_DATA_HOME, or CURSOR_CONFIG_DIR. Cursor has no override. XDG_CONFIG_HOME is a platform default for cursor and for cursor-cli. XDG_STATE_HOME is that kind of input for terva.

### Callers

countSources takes the harness map the caller already loaded. loadAgent, status, and agent status pass file.Harnesses. agent status still prints the home labels for the harnesses it will read.

go test ./internal/cli/ ./internal/config/ -count=1 passed. Schema was not changed.
