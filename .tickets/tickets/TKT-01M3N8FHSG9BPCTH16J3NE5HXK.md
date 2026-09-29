---
schema: 3
id: TKT-01M3N8FHSG9BPCTH16J3NE5HXK
title: "Catalog: project first-seen sightings and lake-wide hidden projects"
type: task
status: ready
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
updated_at: 2026-09-29T01:14:42Z
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

- [ ] Each inventory records first and last seen per device and project
- [ ] Hide and unhide are batched, transactional and audited
- [ ] ReviewQueue groups refused projects lake-wide with hidden state and first seen
- [ ] Backup, fsck and device purge cover the new tables
