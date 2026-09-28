---
schema: 3
id: TKT-01M3MAJ0T4MN38TC6BERD36RD7
title: "Golive legacy-upgrade drill fails: schema 4 rollback leaves newer tables"
type: bug
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/ci
  - area/catalog
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-28T15:36:25Z
updated_at: 2026-09-28T15:36:25Z
created_by:
  id: agent:claude-code/2cf53976
  name: ""
updated_by:
  id: agent:claude-code/2cf53976
  name: ""
extensions: {}
---

## Description

`TestGoLiveOnboardLegacyUpgrade` (`-tags golive`) fails on `main` at f50c57f with "serve exited before listening". This was confirmed on 2026-09-28 in a clean worktree with isolated XDG directories.

### Cause

`legacyCatalog` in `internal/cli/golive_onboard_test.go` rolls a catalog back to schema 4 with `DROP TABLE registrations; DROP TABLE devices; PRAGMA user_version = 4`. Migrations 5 onward have added more since the drill was written (`registration actors`, `head_updates`, `audit_outbox`, `storage_samples`, the machine-activity index), and the lake-managed config work adds `device_reports` and `profiles`/`profile_revisions`. `serve` reruns those migrations on start and fails on the first table that already exists.

### Fix

Build the schema 4 catalog by running `migrations[:4]` on an empty file, as `TestProfilesMigration` in `internal/catalog/schema_test.go` does, instead of dropping tables from a current catalog. That way a new migration cannot break the drill again.

Also consider running the golive drills in CI, or in a scheduled job. They are not run anywhere today, which is how this went unnoticed.

## Acceptance criteria

- [ ] TestGoLiveOnboardLegacyUpgrade passes on main
- [ ] The schema 4 fixture is built by running old migrations, not by dropping tables
