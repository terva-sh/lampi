---
schema: 3
id: TKT-01M3N22HEQ6PJ1A9F56ZQN9M8B
title: "Catalog: device override layer with notes, revisions and audit"
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/catalog
  - area/server
assignees: []
milestone: null
parent: TKT-01M3N22HCYT06PPNZFQP3YCCCE
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-28T22:27:24Z
updated_at: 2026-09-28T22:27:24Z
created_by:
  id: agent:claude-code/2cf53976
  name: ""
updated_by:
  id: agent:claude-code/2cf53976
  name: ""
extensions: {}
---

## Description

Store one override per device and apply it as a second layer after the profile in `profileLayers`, so `ResolveProfile` and the signed payload's `layers` name it (for example `["profile:default", "device:dev_…"]`).

- A table keyed by device id holds the override document, a revision, the operator note, the actor and the time. Migrations are append-only.
- The document is a `config.Profile` checked the way a profile is, and the combination follows the rule the epic settles (open question there).
- Writes are guarded by the revision read, as `PutProfileIf` is, and go to the audit log with the note. Removing an override is a write with a note too.
- An empty or whitespace note is refused.
- A revoked device's override stays readable and cannot be changed.
- `serve devices override show|set|clear` covers the host, as `serve profiles` does, for when the dashboard is not available.
- The agent needs no change: it verifies and applies the resolved payload it already fetches, and the version changes when the override does, so the push reaches it within seconds.

## Acceptance criteria

- [ ] ResolveProfile applies a device's override after its profile, and the payload names both layers
- [ ] An override write needs a non-empty note and the revision it read, and is audited
- [ ] Changing an override changes the device's profile version, so the agent fetches it
- [ ] serve devices override show, set and clear work on the host
