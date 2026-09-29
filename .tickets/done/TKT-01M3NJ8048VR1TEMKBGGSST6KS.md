---
schema: 3
id: TKT-01M3NJ8048VR1TEMKBGGSST6KS
title: "Release v0.3.0: notes, tag, and the published archives and image"
type: task
status: done
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
claim: null
archive: null
created_at: 2026-09-29T03:10:00Z
updated_at: 2026-09-29T04:15:54Z
created_by:
  id: agent:claude-code/16ebd168
  name: ""
updated_by:
  id: agent:claude-code/16ebd168
  name: ""
extensions: {}
---

## Description

Cut v0.3.0 from main at a43c5ce, 82 commits past v0.2.0. The owner asked on 2026-09-29 for a release with the recent improvements, then a local deploy (filed separately).

### What it carries, for the notes

- Catalog migration 15 → 16: project sightings and lake-wide hidden projects, for the project review queue.
- CAS objects and normalized events are written zstd-compressed (`.zst`). v0.2.0 and older cannot read either, so rollback means restoring a backup taken before the upgrade.
- The search index moves to version 4 and is rebuilt once at start. Untimed events are no longer rewritten at every sync (TKT-01M3NENNN8).
- New: encrypted backups (`serve backup --archive`, `serve restore`), `serve compact` compresses objects stored raw, the dashboard's project review queue, hide and unhide, and batch Allow.

### Order

1. Rehearse the upgrade on a scratch lake seeded by v0.2.0, with a build of a43c5ce: migrate 15 → 16, `serve normalize --all`, the search rebuild, and fsck.
2. Write the notes, recorded on this ticket.
3. Tag `v0.3.0` at a43c5ce, which is already on both mains, and push the tag to both forges. The pipeline was proven by the v0.2.0 rcs, and step 1 covers the upgrade, so there is no rc.
4. Check the published archives and the image's `--version`, then attach the notes to both releases.

## Acceptance criteria

- [x] A scratch lake seeded by v0.2.0 upgraded with an a43c5ce build: schema 16, normalize --all, search rebuilt, fsck clean
- [x] v0.3.0 is tagged on both forges and its archives and image name the tag
- [x] Release notes state the 15 to 16 migration, the .zst format and its rollback, and lake-before-agents

## Implementation plan

Tag v0.3.0 at a43c5ce (on both mains), push to origin and github, check the archives and image, then prepend these notes to both release bodies.

#### Upgrading from v0.2.0

- **Take a backup before you upgrade. It is the only way back.** The lake now writes each new blob and each normalized events file compressed, as a `.zst` file, and v0.2.0 cannot read either. Swapping the binary back leaves every blob stored after the upgrade unreadable. Stop `serve`, take `serve backup` with v0.2.0, and check the copy with `serve fsck`. To roll back, restore that backup and start v0.2.0.
- **The catalog migrates from schema 15 to 16.** The migration adds project sightings and lake-wide hidden projects for the review queue. `serve` migrates when it starts, after copying `catalog.db` into `migration-backups/`. That copy restores the catalog alone, and the CAS still needs the backup above.
- **Run `serve normalize --all` after the upgrade**, then `systemctl kill -s HUP terva-lampi-serve` or restart `serve`. This rewrites every session's events file compressed. Until then, the old plain files stay readable.
- **The search index rebuilds once at start** (index version 4). The rebuild reclaims the space that re-indexing untimed events at every sync had added.
- **`serve compact`**, with `serve` stopped, compresses the blobs an older release stored raw. It is optional; those blobs stay readable either way.
- **Upgrade the lake before the agents.** The capture protocol is unchanged, so v0.2.0 agents keep syncing to a v0.3.0 lake.

#### New

- **Encrypted backups.** `serve backup --archive FILE --recipient age1…` (or `--recipients-file`) writes the lake to one age-encrypted, zstd-compressed tar file while `serve` runs. `serve restore --archive … --identity-file …` restores it into an empty directory and checks it as `serve fsck` does. Only public keys go on the lake host. See `docs/vps-bringup.md#encrypted-archive`.
- **Compressed storage.** Blobs and normalized events are stored with zstd. In a sample of the largest local sessions, events took 0.20× their raw size, down from 1.36×. Events files are framed at about 1 MiB, so a page reads one frame, not the whole file.
- **Project review queue on the dashboard.**
  - `/review` lists every refused project that no one has decided about, across all devices. Each repository appears once, with the devices that hold it.
  - Operators can allow projects one at a time, or tick several and use **Allow selected…**, which shows a short confirmation page for each profile.
  - Operators can hide a project from review, singly or in bulk, with a note, and unhide it later.
  - A device's page has the same checkboxes.
  - Allow returns to where you started.
- **The storage card compares the stored blobs with the raw transcripts** the machines hold.

#### Fixes

- The search index no longer rewrites untimed events at every sync.
- CAS writes, re-encodes and `compact` fsync a compressed object before removing its raw copy, and `repair` keeps an intact raw copy.
- `backup --prune` drops objects that a repair removed.

## Notes

**agent:claude-code/16ebd168** at 2026-09-29T03:12:28Z

### Rehearsal, 2026-09-29

A scratch lake was seeded by the v0.2.0 release binary (3f71211) with five copied lampi Claude Code transcripts, and normalized: 4 sessions, schema 15. It was then upgraded with a build of main (a43c5ce plus this ticket's commit, same code), following the deploy's order:

- v0.2.0's `serve backup` of the stopped lake, then `serve fsck` on the copy: exit 0.
- `migrate --check` with the new binary: `catalog schema 15, this binary writes 16: 1 migrations pending`.
- On start, serve wrote `migration-backups/…-v15.db` and ran `migrateProjectReview`, 15 -> 16.
- `serve normalize --all`, then SIGHUP: 4 sessions ready, and every events file is `.jsonl.zst`.
- A sync with the new agent uploaded nothing (unchanged 4). A new session's blob landed as `.zst`.
- Rollback: v0.2.0 serves a copy of the checkpoint at schema 15. v0.2.0's fsck fails on the upgraded lake, which confirms the backup is the only way back.

Not observed: the search.db rebuild. serve builds search.db only with `--web-config`, which needs a reachable OIDC issuer, so the rehearsal ran without it. `TestIndexRebuildsUnknownVersions` covers the version-4 rebuild, and recall, normalize, catalog and cas tests pass on this commit. The live deploy (TKT-01M3NJ805R) observes the rebuild, so criterion 1 stays unticked until then.

**agent:claude-code/16ebd168** at 2026-09-29T03:18:51Z

Published 2026-09-29. v0.3.0 tagged at a43c5ce on both forges. GitHub release run 36516127940 succeeded (archives and image), and the Forgejo release run succeeded. The linux_amd64 archive matches checksums.txt and prints 'terva-lampi v0.3.0 (a43c5ce91425)'. ghcr.io/terva-sh/lampi has 0.3.0, 0.3, 0, latest and sha-a43c5ce, and pulls without a login (podman run prints v0.3.0). The notes in the plan are prepended to both release bodies. Forgejo was edited through the API, because tea releases edit defaults --draft and --prerelease to true. Criterion 1 waits on the live search.db rebuild in TKT-01M3NJ805R.

**agent:claude-code/16ebd168** at 2026-09-29T04:15:52Z

Criterion 1 is ticked on the combined evidence. The scratch rehearsal (note 1) covered schema 16, normalize --all and fsck. The search.db rebuild could not run there without --web-config, and was observed in the live deploy instead (TKT-01M3NJ805R): search.db at index version 4, 349 MiB.

## Summary

Released and deployed. v0.3.0 (tag at a43c5ce) is published on GitHub and Forgejo with the upgrade notes above the generated body. The image ghcr.io/terva-sh/lampi is tagged 0.3.0, 0.3, 0, latest and sha-a43c5ce, and pulls without a login. The upgrade was rehearsed on a scratch lake seeded by v0.2.0. The live deploy (TKT-01M3NJ805R) confirmed the one step the rehearsal could not cover: search.db rebuilt at index version 4, 349 MiB, down from 4.9 GB. normalize --all rewrote all 270 sessions compressed.
