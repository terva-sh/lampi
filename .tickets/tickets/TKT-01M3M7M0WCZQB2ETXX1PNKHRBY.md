---
schema: 3
id: TKT-01M3M7M0WCZQB2ETXX1PNKHRBY
title: Store agent profiles in the catalog with revisions and audit
type: task
status: in-progress
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
claim:
  actor: agent:claude-code/2cf53976
  branch: t3code/add-agent-configuration
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-2cf53976
  commit: 740d778b278d87fc0ec7d2721efb170b44665f74
  session: null
  claimed_at: 2026-09-28T15:12:27Z
  expires_at: null
archive: null
created_at: 2026-09-28T14:45:05Z
updated_at: 2026-09-28T15:12:28Z
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
- Import is an explicit command, `serve profiles import FILE`, that loads a profiles file into the catalog as new revisions (owner, 2026-09-28). There is no automatic import on start.
- If `profiles.json` exists in the lake directory, or `--profiles` is given, `serve` does not use it and says so loudly: a warning at start and on every SIGHUP naming the file and pointing to `serve profiles import`, plus a banner on the dashboard's profiles page. The file's presence must never look like configuration that is in force.
- Upgrade path: the first release with catalog profiles serves no profile until the operator imports one. The release notes and `docs/vps-bringup.md` put `serve profiles import` in the upgrade steps, and the startup warning makes a missed step obvious.
- Every write goes through `ParseProfile`, `Validate` and `forbiddenKeys`, the same checks the agent applies.
- Signing and `GET /v1/agent/config` are unchanged, except that they read the catalog.
- `serve profiles list|show|set|delete` on the host, for when the dashboard is not available. Deleting a profile a device uses is refused (name the devices), and `default` cannot be deleted.

### Shaped for device overrides

Resolution is written as a stack of layers: profile, then (later) a device overlay. The signed payload names the layers it was built from. The first version has one layer. Adding the device overlay later means adding a table and a layer, not changing what a version means.

### Notes on overrides

When device overrides are added, each override carries an operator note saying why it exists (owner, 2026-09-28). Give profile revisions an optional note field now, with the same meaning, so the device layer reuses it.

## Acceptance criteria

- [ ] Profiles live in the catalog with a revision per save
- [ ] Resolution is written as layers so a device overlay can be added later
- [ ] serve profiles import FILE loads profiles; a present profiles.json is ignored with a loud warning
- [ ] Profile revisions carry an optional operator note

## Notes

**agent:claude-code/2cf53976** at 2026-09-28T15:00:28Z

Owner decision 2026-09-28: import is an explicit 'serve profiles import FILE' command. A profiles.json that is present is ignored loudly (warning at start and on SIGHUP, dashboard banner) rather than imported automatically. Replaces the 'decide during the work' item and the automatic import in the first draft of the description, and the matching AC is reworded. Also: revisions get an optional operator note, for reuse by device overrides.
