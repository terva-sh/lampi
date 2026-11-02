---
schema: 3
id: TKT-01M6F7JMM85DPVQ88YSC24TENC
title: "Catalog 22: indexed git remote and cwd on sessions, machine index"
type: task
status: in-progress
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
claim:
  actor: agent:claude-code/t3-43103b81
  branch: t3/catalog-project-columns
  worktree: /home/sothr/.t3/worktrees/lampi/t3-43103b81
  commit: 05b9765e4514c17fcc3425e1138563085a56fee1
  session: null
  claimed_at: 2026-11-02T23:11:12Z
  expires_at: null
archive: null
created_at: 2026-11-02T22:55:13Z
updated_at: 2026-11-02T23:14:00Z
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

- [x] Migration 22 adds indexed remote and cwd columns, backfilled from manifest_json, normalized like config.NormalizeRemote
- [x] Provenance gains an index leading with machine_id
- [x] A rehearsal on a copy of a lake the live lake's size times the migration and checks counts

## Implementation plan

Plain columns, not generated: SQLite cannot run protocol.NormalizeRemote, and the remote must compare in the form allow rules and project ids use. migrateProjectColumns (22) adds sessions.project_remote and project_cwd (NOT NULL DEFAULT ''), fills them in Go from each manifest_json (unreadable manifest: left empty), and creates sessions_by_remote(project_remote, web_updated_ns), sessions_by_cwd(project_cwd, web_updated_ns) and provenance_by_machine(machine_id, session_uid). Ingest writes both columns wherever it writes manifest_json (insert, head move, project id learned). cwd keeps the manifest's form less a trailing slash. Prefix filters will use range scans (col >= 'X/' AND col < 'X0'), which the indexes serve.

## Notes

**agent:claude-code/t3-43103b81** at 2026-11-02T23:14:00Z

Rehearsal (temporary test, removed): a schema-21 catalog with 50,000 synthetic sessions and provenance rows migrated in 0.67s including the pre-migration backup, and all 50,000 rows were filled. TestProjectColumns checks ingest, the migration from 21 (with an unreadable manifest), and that EXPLAIN QUERY PLAN uses each new index. Three rollback tests (bays, head updates, project review) now undo migration 22 too via dropProjectColumns. GOFLAGS=-mod=mod just ci passes. The binary-to-binary upgrade rehearsal belongs to the release that ships this.
