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
updated_at: 2026-09-29T05:25:42Z
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

- [x] A scratch lake seeded by v0.3.0 upgraded with a 1740c08 build: schema 17, counts kept, fsck clean, agents sync nothing new
- [ ] v0.4.0 is tagged on both forges and its archives and image name the tag
- [x] Release notes state the 16 to 17 migration and its rollback, lake-before-agents, and lakes adopt

## Implementation plan

Tag v0.4.0 at 1740c08 (on both mains), push to origin and github, check the archives and image, then prepend these notes to both release bodies.

#### Upgrading from v0.3.0

- **The catalog migrates from schema 16 to 17.** The migration adds `read_tokens`. `serve` migrates when it starts, after copying `catalog.db` into `migration-backups/`. v0.3.0 refuses a schema-17 catalog. To roll back, stop `serve`, restore the catalog from that copy or from a `serve backup` taken before the upgrade, and start v0.3.0. Nothing else on disk changes: blobs, events files and the search index keep their format, so there is nothing to normalize or rebuild.
- **No group becomes admin on upgrade.** Raw artifact reads and read tokens are admin-only. To use them, map a group to `admin` in the web config's `role_map`. A lake with no admin group starts as before and logs a warning.
- **Upgrade the lake before the agents.** The capture protocol is unchanged apart from an added field, so v0.3.0 agents keep syncing to a v0.4.0 lake.

#### New

- **`terva-lampi lakes adopt`** pins a lake that a machine already syncs to with a device token, so the machine takes the lake's profiles. Before this, such a machine never fetched a profile. The machine keeps its machine id, token, device and sync state, and nothing is sent again.
  - The lake's key is checked against a fingerprint you take from `serve identity` on the lake host, then the token is checked, and then the profile is fetched under the pin.
  - Plain `adopt` keeps the local allow rules. `adopt --allow-from profile` then hands them to the profile.
  - It refuses any change that would stop uploading a project or turn off a harness, listing what would stop, unless you pass `--force`.
  - Before uploading projects it did not upload before, it lists them and asks. Without a terminal it refuses unless you pass `--yes`.
  - See `docs/registration-and-lakes.md#adopting-a-lake-a-machine-already-syncs-to`.
- **The dashboard marks devices that fetch no profile.** They are named on the review queue, the devices list and the device page, with the `lakes adopt` hint, and are not offered Allow. Agents now report `pinned`.
- **The admin role**, above operator. It adds:
  - a **Raw** tab on a session's page, which downloads the session's artifacts, capped at 8 MiB with Range support;
  - **read tokens**, minted at `/admin/read-tokens`, which let a tool read raw artifacts through `GET /api/raw/v1/sessions/{uid}/artifacts/{sha256}`. A token is scoped to the lake or to listed sessions and expires within 90 days.

  Every mint, revoke and read is audited.
- **The profile editor previews the effect of an edit.** Changing allow or deny rules lists the projects the change would admit and would stop, taken from each device's newest inventory.
- **The profile editor can shorten the allow list** (**Fewer rules**). It removes rules another rule already covers, and replaces three or more repositories under one owner with a `git_remote_prefix`. Nothing is saved until you save.

## Notes

**agent:claude-code/16ebd168** at 2026-09-29T05:22:42Z

### Rehearsal, 2026-09-29

A scratch lake was seeded by the v0.3.0 release binary (a43c5ce) with a token-file device, a `default` profile and five copied lampi Claude Code transcripts: 5 sessions, 6 artifacts, schema 16. A legacy agent (top-level `server` and `token_file`, local allow rules, no pinned key) synced them. The lake was then upgraded with a build of 1740c08, following the deploy's order:

- v0.3.0's `serve backup` of the stopped lake, then `serve fsck` on the copy: clean, with the same counts as the lake.
- `migrate --check` with the new binary: `catalog schema 16, this binary writes 17: 1 migrations pending`.
- On start, serve wrote `migration-backups/…-v16.db`, ran `migrateReadTokens` and reported `16 -> 17`. Integrity ok and counts unchanged. The anonymous `/v1/stats` returns 401.
- The v0.3.0 agent synced against the v0.4.0 lake and uploaded nothing (unchanged 5).
- `lakes adopt` with the v0.4.0 binary:
  - A wrong fingerprint was refused at check 3, and nothing was written.
  - With the fingerprint from `serve identity`, it pinned the lake and kept the same device (`token-1`), local rules and machine id. The sync that followed uploaded nothing.
  - `--allow-from profile` removed the local allow rule, and `agent config` then showed `allow_source=lake:default`. The sync that followed uploaded nothing.
- Rollback: v0.3.0 refuses the upgraded catalog (`schema 17 is newer than this binary's 16`) and serves a copy of the checkpoint at schema 16.

It ran without `--web-config`, so the admin-groups line at start was not seen. That line is covered by the webconfig tests and is reported by the live deploy.
