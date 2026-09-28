---
schema: 3
id: TKT-01M3M7M0WCZQB2ETXX1PNKHRBY
title: Store agent profiles in the catalog with revisions and audit
type: task
status: done
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
updated_at: 2026-09-28T15:31:59Z
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

- [x] Profiles live in the catalog with a revision per save
- [x] Resolution is written as layers so a device overlay can be added later
- [x] serve profiles import FILE loads profiles; a present profiles.json is ignored with a loud warning
- [x] Profile revisions carry an optional operator note

## Implementation plan

Three stacked PRs.

- **A (catalog-tables):** `profiles` and `profile_revisions` tables (migration 13), the catalog API, and `profile.put`/`profile.delete` audit events.
- **B (catalog-resolver):** a layered resolver, with every profile reader moved to the catalog and `profiles.json` loading removed.
- **C (import-warning):** `serve profiles list|show|set|delete|import`, plus a warning at start and on SIGHUP when a profiles file is present but not in force. Operator docs, including the upgrade step.

A and B were written by a subagent in its own worktree. C was written in the main session.

## Notes

**agent:claude-code/2cf53976** at 2026-09-28T15:00:28Z

Owner decision 2026-09-28: import is an explicit 'serve profiles import FILE' command. A profiles.json that is present is ignored loudly (warning at start and on SIGHUP, dashboard banner) rather than imported automatically. Replaces the 'decide during the work' item and the automatic import in the first draft of the description, and the matching AC is reworded. Also: revisions get an optional operator note, for reuse by device overrides.

**agent:claude-code/2cf53976** at 2026-09-28T15:31:59Z

Design notes from the implementation (A and B):

- **Deletes are revisions.** A delete is stored as a revision flagged `deleted` rather than by removing rows, so each name's history reads in order and a later import or rollback can pick up from it.
- **Revision ids are auto-increment integers,** so an audit line can point at one. Audit events carry no diff, because the revisions table holds the history.
- **An identical save does nothing.** Re-saving the same document creates no revision and no audit event, so re-importing the same file makes no noise. The rejected alternative, a revision on every save, would only have recorded a note on unchanged content.
- **The resolver is a list of layer functions,** each applied on top of the previous result. Two alternatives were rejected:
  - A fixed profile-plus-override struct: adding a layer would have changed the function's shape.
  - A generic merge engine: nothing needs one yet.
- **`AgentConfigPayload.layers` is informational** and is not part of the version. Folding layer names into the version would have changed what a version means and broken agents that recompute it. A test confirms agents accept a payload with `layers` and an unknown field.
- **Redeem passes the resolved profile to its `finish` callback.** The catalog has one connection, so a second query inside the redeem transaction would deadlock. Resolving before Redeem was also rejected, because it would sign outside the transaction that spends the code.
- **`profiles.json` loading was removed outright** instead of kept as a fallback, following the owner's decision never to auto-import. For that reason B must merge together with C.

Known gaps, not fixed:

- A pending registration code that names a deleted profile is not blocked. Redeeming it answers 503 and the code stays valid.
- `devices set-profile` checks the profile, then sets it, in two steps. A concurrent delete between them is possible; the device then gets a 404 on fetch and keeps its cached copy.
- The dashboard banner for an ignored `profiles.json` needs a profiles page, so it moves to TKT-01M3M7M0ZY (Dashboard: view, add, edit and remove agent profiles).
- Merge conflict with the heartbeat stack: `device_reports` kept migration 12, profiles became 13, and both table sets were added to the rollback drop list in `head_updates_test.go`.

## Summary

Profiles live in the catalog: one row per profile, a revision per save, each revision with an optional note, and audit events for puts and deletes. A layered resolver serves them, today one profile layer, and the signed payload lists its layers. serve profiles list, show, set, delete and import manage them. serve no longer reads profiles.json and says so loudly at start and on SIGHUP. Upgrading a lake needs 'serve profiles import' with the old file (docs/vps-bringup.md). PRs #69 (tables), #70 (resolver) and the import PR must merge together, or #70 leaves a lake with no way to set a profile. The dashboard banner moved to TKT-01M3M7M0ZY.
