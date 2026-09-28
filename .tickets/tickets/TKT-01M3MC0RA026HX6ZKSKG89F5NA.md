---
schema: 3
id: TKT-01M3MC0RA026HX6ZKSKG89F5NA
title: "Catalog: migrations that are safe for unattended image upgrades"
type: task
status: in-progress
status_reason: null
priority: high
due_on: null
labels:
  - area/catalog
  - area/server
  - area/ops
assignees: []
milestone: null
parent: TKT-01M3MC023P4A5H7PTF662QSSM8
origin: null
dependencies: []
blocks_on: none
references: []
claim:
  actor: agent:claude-code/aa1afd80
  branch: self-host/migrations
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-aa1afd80
  commit: 9c2bf223863c9356f0ab903aa64cc8f1e66fdabe
  session: null
  claimed_at: 2026-09-28T17:48:15Z
  expires_at: null
archive: null
created_at: 2026-09-28T16:01:57Z
updated_at: 2026-09-28T17:53:56Z
created_by:
  id: agent:claude-code/aa1afd80
  name: ""
updated_by:
  id: agent:claude-code/aa1afd80
  name: ""
extensions: {}
---

## Description

The catalog migrates itself when `serve` opens it: `upgrade` in `internal/catalog/catalog.go` runs each step in `migrations` above `PRAGMA user_version`, one transaction per step, and refuses a file newer than the binary knows about. Derived stores also change between releases: the normalized JSONL, parquet, and `search.db` are regenerated through the published normalization generations and `serve normalize --stale`.

That works for one hand-managed systemd unit. In a container setup, an upgrade is a new tag being pulled, often by Watchtower, Renovate, or `docker compose pull && up -d` with nobody watching. Three things go wrong there:

- **No way back.** After a new image migrates the catalog, the old image refuses to start. A container restart policy then loops on that failure.
- **Two writers during a roll-out.** A Kubernetes `RollingUpdate` or a blue/green start runs the new `serve` while the old one still holds `lake.lock`. The new one fails, or, with the lock on a network filesystem, both run.
- **No record of what changed.** The operator can't tell from the logs whether an upgrade migrated anything or queued re-normalization.

### Work

1. **Log the migration.** At start, `serve` logs the catalog's schema version before and after, one line per step it applies, and whether a normalization generation change queued work. The `lampi_catalog_schema_version` metric already exposes the result.
2. **Back up before a migration.** Decide whether `serve` should take an automatic pre-migration copy of `catalog.db` (a `VACUUM INTO` next to it, named with the old and new versions) when there are steps to apply, or whether the docs should require `serve backup` before an upgrade. The catalog is small next to the CAS, so the automatic copy is cheap. Record the choice and the reasons.
3. **Check without migrating.** Add a subcommand, for example `serve migrate --check` / `serve migrate`, that reports pending steps without applying them, and applies them without starting the listener. It must take `lake.lock`. This lets Kubernetes run migration as an init container and lets an operator check an image before switching to it.
4. **Rolling back.** Document the one supported rollback: stop, restore the catalog from the pre-migration copy or a backup, and start the old tag. Make the downgrade refusal message say this.
5. **Upgrade test.** Add a test that opens a catalog written by the previous release's schema version with the current binary, and one that refuses a newer one. If practical, add an image-level test in CI that starts the previous published tag on a volume, then the new tag.
6. **Release notes.** Every release that appends to `migrations` or changes a normalizer generation says so in its release notes, so someone pinning a tag can see it before pulling.

The single-writer rule becomes deployment guidance in the docs and Kubernetes tickets: one replica, `Recreate` strategy, a lake on local storage.

## Acceptance criteria

- [x] serve logs the schema version before and after, and each migration step it applies
- [x] The pre-migration backup decision is recorded, and implemented if chosen
- [x] A subcommand reports pending migrations and applies them under lake.lock without starting the listener
- [x] The downgrade refusal names the supported rollback path
- [x] A test opens the previous schema version with the current binary
- [x] The release checklist calls out catalog migrations and normalizer generation changes

## Implementation plan

### Decisions

**Automatic backup before a migration.** The owner chose this on 2026-09-28.
- When `catalog.Open` finds a file with tables whose schema is older than the binary's, it first writes a consistent copy with `VACUUM INTO` to `migration-backups/catalog-<UTC time>-v<old>.db` in the lake directory, mode 0600.
- A failed copy stops the migration and `serve` doesn't start: an upgrade with no way back is worse than an upgrade that waits.
- The newest three copies are kept, and older ones are removed. The catalog is small next to the CAS, and three survive a few quick upgrades in a row.
- Alternative considered: telling operators to run `serve backup` first. It lost because an unattended `docker compose pull` or a Renovate bump has no operator in the loop.

**Only a process that holds `lake.lock` migrates.** `serve devices`, `serve register`, `serve profiles`, `serve normalize` and `conflicts` open the catalog for writing without the lock, and today a newer binary's admin command would migrate the schema under an older running `serve`. They move to `catalog.OpenCurrent`, which refuses any schema but the binary's own and says how to upgrade. `serve`, `serve migrate` and the lock-holding maintenance commands (through `api.OpenIdle`) still migrate.

### Work

1. **Logging.** `catalog.Open` records what it did (from, to, the backup path, and each step's number and name). `serve` prints the schema line at every start and one line per step when it migrates.
2. **`serve migrate [--check] [--data DIR]`.**
   - `--check` reads the version read-only, without the lock, so it works beside a running `serve`. It reports up to date, the pending count, or newer than this binary, which is an error.
   - Without `--check`, it takes `lake.lock`, migrates with the backup, and exits without starting a listener. This is for a Kubernetes init container, or a manual step before switching images.
3. **Downgrade refusal.** The newer-schema error names the rollback: stop `serve`, restore `catalog.db` from `migration-backups/` or a `serve backup`, then start the older version.
4. **Tests.**
   - A catalog built at every earlier version upgrades, and leaves a backup at the old version.
   - A fresh catalog makes no backup.
   - Retention keeps three.
   - `OpenCurrent` refuses an older schema.
   - `serve migrate` refuses while `lake.lock` is held.
5. **Releases.** `docs/development.md#releases` says to name catalog migrations and normalizer generation changes in the release notes.

Logging normalizer-generation changes at start-up is left to the docs ticket. `serve normalize --status` already reports what is queued.

## Notes

**agent:claude-code/aa1afd80** at 2026-09-28T17:53:56Z

### Implemented

**`internal/catalog/migrate.go`**
- `upgrade` returns a `Migration` (from, to, backup, step names, created).
- Before the first step, a catalog that holds data is copied with `VACUUM INTO` to `migration-backups/catalog-<UTC>-v<old>.db`, mode 0600. The newest three copies are kept, and a failed copy stops the upgrade.
- A newly created file makes no copy, and `serve` logs `catalog schema N, created` for it.
- `OpenCurrent` refuses any schema but the binary's own. `FileVersion` reads the version read-only.
- `CreateAtVersion` builds a file at an older version, for tests.

**`serve`** logs `catalog schema N, up to date` at every start. When it migrates, it logs the backup path, one line per step (`catalog migration 14: migrateProfiles`), and `catalog schema 13 -> 14`.

**`serve migrate [--check]`.** Without `--check`, it takes `lake.lock`, then migrates and exits. `--check` is read-only and runs beside `serve`.

**Lock-free writers no longer migrate.** `serve devices`, `register`, `profiles`, `normalize`, and `conflicts` use `OpenCurrent`. `serve compact --dry-run`, which opens through `api.OpenIdle` without the lock, now checks the schema first. The read-only paths (`serve devices` list, `normalize --status`) still run against an older catalog and don't change it.

**Docs.** `docs/cli.md`, an Upgrade section in `docs/vps-bringup.md` with the rollback, and a release-notes rule in `docs/development.md#releases`.

**Tests**
- Every earlier schema version (1 to 13) upgrades, keeps its one session row, and leaves a backup at the old version.
- A new catalog and a current catalog make no backup.
- Retention keeps three and leaves unrelated files alone.
- `OpenCurrent` refuses an older schema and leaves it untouched.
- The newer-schema error names the rollback.
- `serve migrate --check` works while the lock is held and changes nothing.
- `serve migrate` refuses while the lock is held, then upgrades.
- Six lock-free commands leave an older catalog at its version, with no backup.

### Deviations

- The rollback doesn't recover manifests accepted after the upgrade. The docs say to roll back soon. I didn't check whether an agent re-sends such a session, so the docs don't claim it does.
- Logging normalizer-generation changes at start is not done, as the plan said.
- A local `just ci` failed once on `TestAgentReportsItsSyncAndProfileToTheLake` and passed on the re-run. The test isn't related to this change, and I filed it as a draft bug.
