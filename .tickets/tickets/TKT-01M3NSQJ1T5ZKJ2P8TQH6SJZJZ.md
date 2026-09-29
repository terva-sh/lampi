---
schema: 3
id: TKT-01M3NSQJ1T5ZKJ2P8TQH6SJZJZ
title: "Release v0.4.0: notes, tag, and the published archives and image"
type: task
status: in-progress
status_reason: null
priority: high
due_on: null
labels:
  - area/ci
  - area/ops
  - area/docs
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim:
  actor: agent:claude-code/16ebd168
  branch: release/v0.4.0
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-16ebd168
  commit: 1740c088c9eda3de9afcf565aeb14fc586dd297f
  session: null
  claimed_at: 2026-09-29T05:20:58Z
  expires_at: null
archive: null
created_at: 2026-09-29T05:20:50Z
updated_at: 2026-09-29T05:20:58Z
created_by:
  id: agent:claude-code/16ebd168
  name: ""
updated_by:
  id: agent:claude-code/16ebd168
  name: ""
extensions: {}
---

## Description

Cut v0.4.0 from main at 1740c08, nine merges past v0.3.0. The owner asked on 2026-09-29 for a release that carries `lakes adopt`, so the fleet's machines that sync with a plain device token can be adopted, and for it to be deployed locally (filed separately).

### What it carries, for the notes

- Catalog migration 16 → 17: `read_tokens`, for raw-read tokens. v0.3.0 refuses a schema-17 catalog, so rollback means restoring the catalog as it was before the upgrade.
- No change to the CAS, the events files or the search index. Nothing to normalize or rebuild.
- The agent report gains `pinned` (additive), so a v0.3.0 agent keeps syncing to a v0.4.0 lake.
- New: `terva-lampi lakes adopt` (TKT-01M3NMHDWR); the admin role, the admin-only raw artifact view and raw-read tokens (TKT-01M3NM61CZ); the profile editor's preview of projects a change admits and drops (TKT-01M3NM01K), and its covered-rule and owner-group tidy (TKT-01M3NM01N).
- No group is promoted to admin on upgrade. A lake whose `role_map` names no admin group keeps working as before, and serve warns at start.

### Order

1. Rehearse on a scratch lake seeded by v0.3.0, upgraded with a build of 1740c08: migrate 16 → 17, counts and fsck, a v0.3.0 agent and a v0.4.0 agent sync nothing new, and `lakes adopt` pins a legacy agent against it.
2. Write the notes, recorded on this ticket.
3. Tag `v0.4.0` at 1740c08, which is on both mains, and push the tag to both forges. No rc: the pipeline is unchanged since v0.3.0.
4. Check the published archives and the image's `--version`, then prepend the notes to both releases.

## Acceptance criteria

- [ ] A scratch lake seeded by v0.3.0 upgraded with a 1740c08 build: schema 17, counts kept, fsck clean, agents sync nothing new
- [ ] v0.4.0 is tagged on both forges and its archives and image name the tag
- [ ] Release notes state the 16 to 17 migration and its rollback, lake-before-agents, and lakes adopt
