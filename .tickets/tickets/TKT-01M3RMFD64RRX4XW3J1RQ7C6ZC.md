---
schema: 3
id: TKT-01M3RMFD64RRX4XW3J1RQ7C6ZC
title: "Release v0.5.0: bays and conflicts; notes, tag, archives and image"
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
  actor: agent:claude-code/27b21f4b
  branch: release/v0.5.0
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-27b21f4b
  commit: 85480f57367c8652a4bbbd40b6aa07066c72fd34
  session: null
  claimed_at: 2026-09-30T19:41:36Z
  expires_at: null
archive: null
created_at: 2026-09-30T07:46:43Z
updated_at: 2026-09-30T19:41:36Z
created_by:
  id: agent:claude-code/fdd1a9d1
  name: ""
updated_by:
  id: agent:claude-code/27b21f4b
  name: ""
extensions: {}
---

## Description

Cut the next release from main. It carries the bays epic (TKT-01M3N8KHW5, Bays: segment one lake and route sessions to a bay) and the conflicts epic (TKT-01M3PTMA). The owner deferred the cut on 2026-09-30 ("leave the cut for later") and signed off the bays policy the same day, so nothing that was blocking remains. This ticket holds the pre-release checks and the notes until the owner asks for the tag.

v0.5.0 is proposed, not decided: the catalog schema moves by four and bays add an access model, which is more than a patch.

### What it carries, for the notes

- Catalog migrations 17 → 21:
  - 18 `migrateConflictResolutions`
  - 19 `migrateBays`
  - 20 `migrateBayScopes`
  - 21 `migrateBayRules`

  v0.4.0 refuses a schema-21 catalog, so rollback means restoring the catalog as it was before the upgrade.
- No change to `internal/normalize`, the CAS or the search index. Nothing to normalize or rebuild.
- The manifest gains an optional `bays` field and `hello` lists writable bays. Both are additive, so `capture_protocol` stays 1 and a v0.4.0 agent keeps syncing.
- Migration 18 resolves the old false conflicts (Claude subagent transcripts recorded as divergent copies) as `not_a_conflict`. The live lake has 184 of them, which the agent user cannot read to confirm in advance; check the Conflicts page after the deploy.

### Order

1. Rehearse on a scratch lake seeded by v0.4.0 and upgraded with a build of main. Done at 0d9d06b; see the note. Rerun it if main moves before the tag.
2. Write the notes, recorded in the plan.
3. Tag at a commit on both mains and push the tag to both forges.
4. Check the published archives and the image's `--version`, then prepend the notes to both releases.

Deploying to the internal lake and the workstation agent is a separate ticket, filed when the owner asks, as TKT-01M3NSQJ3 was for v0.4.0.

## Acceptance criteria

- [x] A scratch lake seeded by v0.4.0 upgraded with a main build: schema 21, counts kept, every session in default, fsck clean, agents sync nothing new
- [ ] The release is tagged on both forges and its archives and image name the tag
- [ ] Release notes state the 17 to 21 migration and its rollback, the bays upgrade grants, lake-before-agents, and the conflict cleanup

## Implementation plan

Tag at a commit on both mains after the owner asks for the cut, push to origin and github, check the archives and image, then prepend these notes to both release bodies.

#### Upgrading from v0.4.0

- **The catalog migrates from schema 17 to 21.** `serve` migrates when it starts, after copying `catalog.db` into `migration-backups/`. v0.4.0 refuses a schema-21 catalog. To roll back, stop `serve`, restore the catalog from that copy or from a `serve backup` taken before the upgrade, and start v0.4.0. Blobs, events files and the search index keep their format, so there is nothing to normalize or rebuild.
- **Upgrading changes no one's access.** Every stored session goes into the `default` bay and every device keeps write on it. Each group the web config maps to viewer or operator is granted read on `default`, and operator groups also write, so they can still add machines. `serve` logs each grant at the first start. No group becomes admin.
- **Upgrade the lake before the agents.** The protocol additions are optional fields, so v0.4.0 agents keep syncing to this lake. Their sessions land in `default` unless a lake rule places them.
- **Old false conflicts are resolved.** Claude subagent transcripts that earlier releases recorded as divergent copies are marked "not a conflict" by the migration, with an audit line each, and leave the Conflicts page.

#### New

- **Bays.** A lake can be split into named bays, and each bay is an access boundary (`docs/policy.md#bays`).
  - Viewers, operators, registration codes and read tokens can be limited to bays. Every read path is scoped by the caller's bays.
  - An agent can ask for bays per lake, and the lake's hold, add and deny rules apply on top. `terva-lampi bays` lists a machine's writable bays, and `terva-lampi bays which [PATH]` explains where a session would go.
  - `serve bays` manages bays, grants, rules and holds. `serve bays inbox` lists sessions nobody has sorted, with a reason each. Bulk `move` and `apply-rules` take `--dry-run`. `docs/bays-inbox.md` covers sorting a lake after the upgrade.
  - The dashboard shows a session's bays, and admins get an inbox where they can move a session or release a hold.
- **Conflicts you can settle.** The Conflicts page explains each row: the machine behind each copy, a shorter copy, how many copies sit at the path, and where the two files part. An operator can keep the head, reopen a conflict, or make a copy the head. Every action is audited. `GET /v1/conflicts?resolved=` and `terva-lampi conflicts --resolved` list settled conflicts.
- **Allow picks its width.** Allowing a project on the review queue lets you choose the repository or its whole owner.
- **`cwd_glob`** joins the allow and deny rule fields.

## Notes

**agent:claude-code/fdd1a9d1** at 2026-09-30T07:46:43Z

### Rehearsal, 2026-09-30

A scratch lake was seeded by the v0.4.0 release binary (1740c08) with a token-file device, a web config mapping `lake-viewers` to viewer and `lake-operators` to operator with no admin group, and five copied lampi Claude Code transcripts: 5 sessions, 6 artifacts, schema 17. A legacy agent (top-level `server` and `token_file`) synced them. The lake was then upgraded with a build of 0d9d06b:

- v0.4.0's `serve backup` of the stopped lake, then `serve fsck` on the copy: clean.
- `migrate --check` with the new binary: `catalog schema 17, this binary writes 21: 4 migrations pending`.
- On start, serve wrote `migration-backups/…-v17.db`, ran migrations 18 to 21 and reported `17 -> 21`. Integrity ok. Sessions, artifacts, provenance and devices unchanged. The anonymous `/v1/stats` returns 401.
- Bays after the upgrade:
  - one bay, `default`, holding all 5 sessions;
  - the device has write on `default`;
  - `lake-viewers` has read and `lake-operators` read and write, each logged at start;
  - no group has admin, and serve warned that raw reads are off.

  `serve bays inbox` lists each session with "no bay asked for and no rule added one".
- The v0.4.0 agent and a 0d9d06b agent each synced against the upgraded lake and uploaded nothing (unchanged 5). `terva-lampi bays` shows `default: bays: default`.
- `serve fsck` on the upgraded lake: 6 entries, 0 bad.
- Rollback: v0.4.0 refuses the upgraded catalog (`schema 21 is newer than this binary's 17`) and serves a copy of the checkpoint at schema 17.

Not covered: the scratch lake had no divergent copies, so migration 18 resolved nothing here. Its selection is covered by `internal/catalog/conflicts_test.go`. The live lake's 184 rows get checked on the Conflicts page after the deploy. Nobody signed in, so the dashboard under bays was not exercised; the route tests and the smoketest cover it.

**agent:claude-code/27b21f4b** at 2026-09-30T19:41:36Z

Owner asked for the cut on 2026-09-30 and confirmed v0.5.0. Tagging 85480f5, which is main on both forges (Forgejo and GitHub CI green). No code changed since the rehearsal at 0d9d06b, only .tickets/, so the rehearsal stands. No internal/normalize diff since v0.4.0 and no agent advisory.
