---
schema: 3
id: TKT-01M6F7JMM85DPVQ88YSC24TENC
title: "Catalog 22: indexed git remote and cwd on sessions, machine index"
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/catalog
assignees: []
milestone: null
parent: TKT-01M6F7JMD2S5S9Q418SYSYDM34
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-11-02T22:55:13Z
updated_at: 2026-11-02T22:55:13Z
created_by:
  id: agent:claude-code/t3-43103b81
  name: ""
updated_by:
  id: agent:claude-code/t3-43103b81
  name: ""
extensions: {}
---

## Description

Step 2a of the epic: catalog migration 21 → 22. The owner chose indexed columns over an in-memory cache.

Add stored generated columns on `sessions`, or columns filled at insert and by a backfill, for the project's git remote, normalized as `config.NormalizeRemote` does, and for its cwd. Both come from `manifest_json`'s `$.project`. Index each column, and add an index that leads with `machine_id` on provenance for the machine filter.

Check first whether SQLite's generated columns can carry the normalization. If they cannot, use plain columns written by the insert path, plus a backfill in the migration.

### Release notes must say

- the catalog moves to schema 22;
- `serve` backs up the catalog to `migration-backups/` first;
- rolling back means restoring that backup.

### Rehearsal

Migrate a copy of a lake the size of the live one, timing it and checking the backfill counts.

## Acceptance criteria

- [ ] Migration 22 adds indexed remote and cwd columns, backfilled from manifest_json, normalized like config.NormalizeRemote
- [ ] Provenance gains an index leading with machine_id
- [ ] A rehearsal on a copy of a lake the live lake's size times the migration and checks counts
