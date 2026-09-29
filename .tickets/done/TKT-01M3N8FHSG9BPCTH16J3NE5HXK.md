---
schema: 3
id: TKT-01M3N8FHSG9BPCTH16J3NE5HXK
title: "Catalog: project first-seen sightings and lake-wide hidden projects"
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/catalog
assignees: []
milestone: null
parent: TKT-01M3N8F354GDK9D0CTQ2BMZBMY
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-29T00:19:22Z
updated_at: 2026-09-29T01:23:45Z
created_by:
  id: agent:claude-code/10adf304
  name: ""
updated_by:
  id: agent:claude-code/10adf304
  name: ""
extensions: {}
---

## Description

The lake keeps only each device's newest inventory (`device_inventories`). It can't say when a project first appeared, and it has nowhere to record that the operator chose not to import one. The review queue needs both.

### Project sightings

- On `PutDeviceInventory`, upsert each listed project for that device into a sightings table: `(device_id, key_kind, key)`, with `first_seen` and `last_seen` taken from the lake's clock. `key_kind` is `git_remote` or `cwd`. The key is what `allowRule` would produce: the folded remote, or the cwd when there is no remote.
- A project that drops out of a later inventory keeps its row. `last_seen` shows it went away.
- A strict-mode inventory names no refused project and adds no refused rows.
- A device's first inventory after the upgrade makes every project it lists look new. Record the migration time in `lake_meta` so the page can say "first seen at or before …" for those rows.

### Hidden projects

- A lake-wide table keyed by `(key_kind, key)`, with `hidden_by`, `hidden_at` and an optional note up to 500 characters, like revision notes.
- `HideProjects` and `UnhideProjects` take a batch in one transaction. Each writes to `audit.jsonl` through the outbox with the operator as actor.
- Hiding sends nothing to agents and changes no profile.

### Query

`ReviewQueue` returns the refused projects across every active device's newest inventory, grouped by key. Each group has its devices, total sessions and bytes, newest session, reasons, earliest `first_seen`, and whether it is hidden. It also returns per-device strict-mode refused totals. The web layer then drops groups the device's current profile already permits and marks them *allow pending*, since the agent has not fetched the rule yet.

`serve fsck` and backup cover the new tables. `serve purge` of a device removes its sightings.

## Acceptance criteria

- [x] Each inventory records first and last seen per device and project
- [x] Hide and unhide are batched, transactional and audited
- [x] ReviewQueue groups refused projects lake-wide with hidden state and first seen
- [ ] Backup, fsck and device purge cover the new tables

## Implementation plan

Migration 16 (migrateProjectReview) adds project_sightings (device, key kind, key → first/last seen ns) and hidden_projects (key kind, key → by, when, note). It seeds sightings from the stored inventories at their received time and records sightings_since in lake_meta. PutDeviceInventory now runs in a transaction and, when it keeps an inventory, upserts every listed project's sighting at the lake's clock with min(first)/max(last). ProjectKeyOf mirrors the Allow rule's key: NormalizeRemote(remote), else trimmed cwd. HideProjects and UnhideProjects validate every key before writing, run in one transaction and queue project.hidden and project.unhidden. ReviewQueue groups refused rows from active devices' newest inventories by key, sorted by first seen, newest first, and carries hides and strict-device totals. The web layer decides which projects a profile already covers. Rejected: computing first-seen from inventory generated_at, the agent's clock. The queue orders by when the lake learned of a project.

## Notes

**agent:claude-code/10adf304** at 2026-09-29T01:23:45Z

AC 4 (backup, fsck and device purge cover the new tables) was written before reading the code, and two of its three parts name things that don't exist. Backup is VACUUM INTO, so it copies every catalog table, the new ones included, with no change needed. serve fsck re-hashes CAS objects and checks identity.json but checks no catalog table. There is no device purge: devices are revoked and never deleted, and ReviewQueue skips revoked devices. So there was nothing to add, and the box stays unticked rather than claim work that wasn't done. TestHeadUpdatesMigrationInventsNothing now also drops the two new tables when it rebuilds schema 7.

## Summary

internal/catalog/review.go: migration 16 with project_sightings and hidden_projects (sightings seeded from stored inventories), sightings recorded on every kept inventory, batched and audited HideProjects/UnhideProjects, and ReviewQueue. New audit kinds project.hidden and project.unhidden. Tests: TestInventoriesRecordWhenEachProjectWasFirstSeen, TestReviewQueueGroupsRefusedProjectsAcrossDevices, TestHideAndUnhideProjects, TestProjectReviewMigrationSeedsSightings. AC 4 is unticked; see the note for why it did not apply.
