---
schema: 3
id: TKT-01M3MC0RA026HX6ZKSKG89F5NA
title: "Catalog: migrations that are safe for unattended image upgrades"
type: task
status: draft
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
claim: null
archive: null
created_at: 2026-09-28T16:01:57Z
updated_at: 2026-09-28T16:01:57Z
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

- [ ] serve logs the schema version before and after, and each migration step it applies
- [ ] The pre-migration backup decision is recorded, and implemented if chosen
- [ ] A subcommand reports pending migrations and applies them under lake.lock without starting the listener
- [ ] The downgrade refusal names the supported rollback path
- [ ] A test opens the previous schema version with the current binary
- [ ] The release checklist calls out catalog migrations and normalizer generation changes
