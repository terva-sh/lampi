---
schema: 3
id: TKT-01M39306SKH81TGQ69HSV578FF
title: "Agent: honor harness enable + root"
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
updated_at: 2026-09-24T08:07:12Z
created_by:
  id: agent:atlas/architect
  name: Atlas - Architect
updated_by:
  id: agent:cursor/6d91
  name: Cursor cloud agent
extensions: {}
---

## Description

Wire `terva-lampi agent` / sync discover, watch, and upload to Shape A config.

For each known harness, resolve root with precedence flag > config `root` > env > adapter default. If `enabled` is false, skip discover, watch, and upload for that harness only. Do not delete or rewrite watermarks. Do not touch CAS objects.

## Acceptance criteria

- [x] Disabled harness is absent from discover/watch/upload paths; other harnesses unchanged
- [x] Config `root` overrides env and adapter default; env still overrides default when config `root` is omitted
- [x] Disabling a harness does not clear watermarks or mutate CAS
- [x] Re-enabling a harness resumes from existing watermarks
- [x] Tests cover enable=false skip and root precedence (config beats env beats default)

## Definition of done

- [x] Agent/sync path honors the schema from TKT-01M39306SHCKX7FD61WJQB2PDZ
- [x] `go test ./...` green for touched packages
- [x] No VPS, restic, frontend, purge, or soft-link work

## Implementation plan

### Where

`sources` in internal/cli/peers.go is the one list discover, watch, and upload already share. It will take `config.Harnesses` from the loaded config file. `resolveHome` sits in front of `adapter.Home` and does not change those signatures.

### Precedence

`resolveHome(id, cfgEntry, flagRoot, getenv, adapterHome)` returns the directory and whether to skip. `cfgEntry` nil means the key was omitted: enabled, no root. A non-nil entry with Enabled false skips. flagRoot is the flag slot. No flag exists, so production passes "". Order is flagRoot, then entry.Root, then adapterHome, which still applies env then the adapter default.

### Skip

A skipped harness is left out of the slice. Discover, startWatches, and homeOf then never see it. That includes terva: enabled false wins over the "missing terva home is still watched" rule. An empty TervaHome skips `terva.Manifests` the way the other homes already skip, so a disabled terva does not walk a relative sessions directory.

### Watermarks

Nothing deletes or rewrites a watermark for a harness that is not in the slice. Re-enabling uses the same root, so the existing cursor is the resume point.

## Notes

**agent:cursor/6d91** at 2026-09-24T08:07:12Z

### For the status ticket

resolveHome in internal/cli/peers.go is the shared helper. cfgEntry nil means the key was omitted. skip is true only when the entry exists and Enabled is false. The home is still returned in that case: flagRoot, then Root, then adapter Home. Production passes flagRoot "". sources drops a skip, so agent discover, watch, upload, agent config, and agent status currently omit a disabled harness. TKT-01M39306SNS6A9VC1RYA8ERTTE (status: resolved harnesses) can call resolveHome for every known id and print enabled from skip.

### Terva required

enabled false omits terva before the required-home check. A disabled terva is not watched and a missing HOME is not an error. An enabled terva with a missing directory is still watched.

## Summary

sources in internal/cli/peers.go now takes the Shape A harnesses map. resolveHome applies flag, then config root, then adapter Home. No root flag exists, so the agent passes an empty flag and Home still does env then the adapter default. Adapter Home signatures are unchanged.

enabled false drops that harness from the slice. Discover, watch, and upload all read the slice, so the harness is absent from all three and the others stay. That includes terva: a disabled terva is not forced onto the watcher. An empty TervaHome skips terva.Manifests, same as the other empty homes, so a disabled terva does not walk a relative sessions directory.

A disabled harness does not rewrite its watermark row and does not add or remove CAS objects. Turning it back on uses the same root, so the next sync sends the new tail. The test records grown_from from the previous digest.

go test ./internal/cli/ ./internal/config/ ./internal/upload/ -count=1 passed. Schema was not changed.
