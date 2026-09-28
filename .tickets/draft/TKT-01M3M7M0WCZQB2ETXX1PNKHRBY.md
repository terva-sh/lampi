---
schema: 3
id: TKT-01M3M7M0WCZQB2ETXX1PNKHRBY
title: Store agent profiles in the catalog with revisions and audit
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area/catalog
  - area/server
assignees: []
milestone: null
parent: TKT-01M3M7KB32E2BCFEA9CN710522
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-28T14:45:05Z
updated_at: 2026-09-28T14:45:05Z
created_by:
  id: agent:claude-code/2cf53976
  name: ""
updated_by:
  id: agent:claude-code/2cf53976
  name: ""
extensions: {}
---

## Description

Move agent profiles from `profiles.json` into the catalog so the dashboard can edit them.

- Tables: profiles (name, document, version, updated_at, updated_by) and a revisions table holding every saved document, so a bad edit can be rolled back and the audit log can show the diff.
- On the first start after the upgrade, import `profiles.json` if the catalog holds no profiles, and log that it did. After that the catalog is the source, and the file is ignored with a warning if it changes. **Decide during the work:** whether `--profiles` stays as an explicit import command (`serve profiles import FILE`) instead.
- Every write goes through `ParseProfile`, `Validate` and `forbiddenKeys`, the same checks the agent applies.
- Signing and `GET /v1/agent/config` are unchanged, except that they read the catalog.
- `serve profiles list|show|set|delete` on the host, for when the dashboard is not available. Deleting a profile a device uses is refused (name the devices), and `default` cannot be deleted.

### Shaped for device overrides

Resolution is written as a stack of layers: profile, then (later) a device overlay. The signed payload names the layers it was built from. The first version has one layer. Adding the device overlay later means adding a table and a layer, not changing what a version means.

## Acceptance criteria

- [ ] Profiles live in the catalog with a revision per save
- [ ] profiles.json is imported once on upgrade
- [ ] Resolution is written as layers so a device overlay can be added later
