---
schema: 3
id: TKT-01M3N22HCYT06PPNZFQP3YCCCE
title: Per-device overrides on top of agent profiles
type: epic
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
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

A device-level layer on top of its profile, so an operator can change what one machine collects without editing the profile every other device on it fetches. Split out of TKT-01M3M7KB (Lake-managed agent config), which deferred overrides and carried their note requirement as its fourth criterion.

### Owner decisions already made (2026-09-28, Drew Short)

- Per-device overrides come after the profile editor. The data model was shaped for them. Resolution is a list of layers (`profileLayers` in `internal/catalog/resolve.go`), the signed payload names its `layers`, and adding the device layer adds a table and a layer without changing what a version means (TKT-01M3M7M0W).
- Every override carries an operator note saying why it exists.
- The Allow action on a device's page, once overrides exist, can target the device layer instead of the profile. When it does, it must ask for the note.
- The profile page reserves a "Device overrides (later)" tab.

### Open question for the owner

How an override combines with the profile. The children assume the one below; confirm or change it before TKT-01M3N22HEQ (Catalog: device override layer with notes, revisions and audit) starts.

- Allow and deny rules are **added to** the profile's, not replacing them.
- Debounce values and harness toggles set by the override **replace** the profile's.
- An override cannot set a harness root, `upload_hits` or the inventory mode, which are closed to profiles for the same reasons.

The rejected alternative is an override that replaces the profile's rule lists whole. It would silently drop a deny rule added to the profile later.

## Acceptance criteria

- [ ] An operator changes one device's config without changing its profile for other devices
- [ ] Every override carries an operator note saying why it exists
- [ ] The device page and the profile's Device overrides tab show each override and its note
- [ ] Allow on a device's page can target the device layer, and then requires a note
