---
schema: 3
id: TKT-01M3B369GXF6A0F51R79GFZM07
title: "Cursor IDE capture: one global snapshot, SQL filter, safe copy"
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/adapter
assignees: []
milestone: null
parent: TKT-01M3B35JS8J2F83FG4J7ZC0199
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-25T01:34:31Z
updated_at: 2026-09-25T01:34:32Z
created_by:
  id: agent:claude-code/eh1m
  name: Claude Code cloud agent
updated_by:
  id: agent:claude-code/eh1m
  name: Claude Code cloud agent
extensions: {}
---

## Description

The Cursor IDE capture copies the global database once per workspace on every sync, and the snapshot can be torn.

### Findings

- Cost. Read. Each workspace export copies the whole global `state.vscdb` trio to `$TMPDIR` (`internal/adapter/cursor/snapshot.go:101,126,298`), reads all of `cursorDiskKV` into memory before filtering, and exports the global database in full, although the allowlist always refuses it (`cursor.go:252`). `state.vscdb-wal` writes trigger syncs.
- Torn snapshot. Read, not reproduced. `copyTrio` (`cursor/snapshot.go:298`, `cursorcli/snapshot.go:355`) copies the database, `-wal`, and `-shm` one after another with no lock. A checkpoint between copies can pair an old main file with a new WAL.

### Approach

Snapshot the global database once per sync. Filter in SQL by composer id. Skip the global export, and any workspace the allowlist refuses by cwd, before exporting. Take snapshots with `VACUUM INTO` or the backup API on a read-only open, or re-stat after the copy, retry on change, and run `quick_check`.

## Acceptance criteria

- [ ] One sync copies the global database at most once
- [ ] Refused workspaces and the global database are not exported
- [ ] A snapshot is taken consistently or retried
