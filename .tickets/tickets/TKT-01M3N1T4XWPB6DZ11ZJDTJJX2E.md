---
schema: 3
id: TKT-01M3N1T4XWPB6DZ11ZJDTJJX2E
title: "Release v0.2.0: notes, rc1, and the first GHCR image"
type: task
status: blocked
status_reason: Waiting on the owner to set the GHCR lampi package public; then check an anonymous pull of ghcr.io/terva-sh/lampi:0.2.0.
priority: high
due_on: null
labels:
  - area/ci
  - area/ops
  - area/docs
assignees: []
milestone: v0.2.0
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim:
  actor: agent:claude-code/aa1afd80
  branch: tickets/release-grooming
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-aa1afd80
  commit: a5a6b474290a6d8c0ee308f2eb8c702f6aab1bdc
  session: null
  claimed_at: 2026-09-28T22:38:34Z
  expires_at: null
archive: null
created_at: 2026-09-28T22:22:49Z
updated_at: 2026-09-28T23:29:35Z
created_by:
  id: agent:claude-code/aa1afd80
  name: ""
updated_by:
  id: agent:claude-code/aa1afd80
  name: ""
extensions: {}
---

## Description

Cut v0.2.0, the first release since v0.1.2. It is also the first release that publishes the lake image to `ghcr.io/terva-sh/lampi`. The process is in `docs/development.md` under "Releases".

A `v0.1.3` tag exists in the local clone, but it was never pushed to either forge and never published, so v0.1.2 is the baseline for these notes.

This ticket drives two others to done, and so does not depend on them:
- TKT-01M3MC0RN (Release CI: publish the multi-arch lake image to GHCR on v* tags), whose criteria are proved only by a real tag;
- TKT-01M3MC0RE (GHCR: set up the terva-sh container package for the lake image), whose package can be made public only after the first publish.

### Order

1. Merge and sync (`just sync-github --yes`), so the commit is on both mains.
2. Write the release notes, recorded on this ticket.
3. Tag `v0.2.0-rc1` on that commit and push the tag to both forges. A hyphenated tag publishes a prerelease, and the image gets only `0.2.0-rc1` and `sha-<short>`, never `latest`.
4. Check the rc:
   - pull the image and run `--version` on amd64 and arm64;
   - verify the attestation as in `docs/container.md`;
   - upgrade a schema-11 lake seeded by the v0.1.2 binary.
5. An owner sets the `lampi` package public in GHCR, which closes TKT-01M3MC0RE.
6. Tag `v0.2.0`, attach the notes to both releases, then upgrade the hosted lake before the agents (TKT-01M3FP11A, "Onboarding rollout: upgrade the hosted lake and register machines").

The release workflow runs `go test ./...` before it publishes. Three tests are known to be flaky under load: TKT-01M3MKF5, TKT-01M3MJDS and TKT-01M3MSE6. If one fails, re-run the job; a flake is not a reason to change the tag.

## Acceptance criteria

- [x] v0.2.0-rc1 published archives and a two-platform image that passed the checks
- [ ] The GHCR package is public and pulls without a login
- [x] v0.2.0 is tagged on both forges with the notes attached
- [x] A v0.1.2 lake upgraded to the rc image and its agents still sync
- [x] Release notes state the schema 11 to 15 migration, its rollback, and lake-before-agents

## Implementation plan

Follow the order in the description. The release notes below are the canonical text to prepend to both release bodies. They replace the two drafts in the notes, including the first draft's schema 10 to 14 line, which was wrong.

#### Upgrading from v0.1.2

- **The catalog migrates from schema 11 to 15.** The migrations add subagent heads, device reports, agent profiles with revisions, and device inventories. `serve` migrates when it starts, after copying `catalog.db` into `migration-backups/` in the lake directory. `serve migrate` does the same without starting the listener, and `serve migrate --check` reports what's pending. v0.1.2 can't open a migrated catalog, so rolling back means stopping `serve`, restoring that copy, deleting `catalog.db-wal` and `catalog.db-shm`, and then starting v0.1.2.
- **Upgrade the lake before the agents.** A new agent sends only the bytes appended to a transcript past 32 MiB, but only to a lake that advertises `large_tails`. Against a v0.1.2 lake it still works, sending whole chunks as before. A new lake also folds a grown file's old last chunk at ingest, so such transcripts no longer grow the lake quadratically between compactions.

#### New

- **A container image** at `ghcr.io/terva-sh/lampi`, for linux/amd64 and linux/arm64, with an SBOM and build provenance. It runs distroless as a non-root user, and a compose example puts Caddy in front of it. See `docs/container.md`. New commands: `serve healthcheck`, `serve migrate`, `serve --behind-proxy`, and `serve backup --prune`, which drops from a backup what a purge or compact removed from the lake.
- **Device fleet on the dashboard.**
  - Each device shows its agent version, with a badge when it's behind or on a release with a known problem.
  - Devices can be revoked, unbound and given a profile from the dashboard.
  - Each device has a page with its reported inventory.
  - Agents gain `terva-lampi self-update`, which installs the lake's release after verifying it.
- **Agent profiles in the lake.** `serve profiles` imports, lists, sets and deletes profiles. The dashboard edits them, keeps each revision and rolls back. Edits reach connected agents within seconds.

#### Fixes

- The search index is reclaimed after each pass, and a new generation writes only changed rows.
- A Claude session keeps its own transcript as its head when it has subagents.
- Search marks only failed tool results as tool errors.
- The deduplication tile divides by the blobs actually on disk.
- Web UI 500s log their cause.
- A rotated IdP key verifies despite a stale JWKS fetch.
- Lake lock errors say to stop `serve` only when `serve` holds the lock.

## Notes

**agent:claude-code/aa1afd80** at 2026-09-28T22:38:34Z

### Release notes for v0.2.0 (draft)

These get prepended to the GitHub and Forgejo release bodies, above goreleaser's header.

#### Upgrading from v0.1.2

- **The catalog migrates from schema 10 to 14.** The migrations add subagent heads, device reports, agent profiles with revisions, and device inventories. `serve` migrates when it starts, after copying `catalog.db` into `migration-backups/` in the lake directory. `serve migrate` does the same without starting the listener, and `serve migrate --check` reports what's pending. v0.1.2 can't open a migrated catalog, so rolling back means stopping `serve`, restoring that copy, deleting `catalog.db-wal` and `catalog.db-shm`, and then starting v0.1.2.
- **Upgrade the lake before the agents.** A new agent sends only the bytes appended to a transcript past 32 MiB, but only to a lake that advertises `large_tails`. Against a v0.1.2 lake it still works, sending whole chunks as before. A new lake also folds a grown file's old last chunk at ingest, so such transcripts no longer grow the lake quadratically between compactions.

#### New

- **A container image** at `ghcr.io/terva-sh/lampi`, for linux/amd64 and linux/arm64, with an SBOM and build provenance. It runs distroless as a non-root user, and a compose example puts Caddy in front of it. See `docs/container.md`. New commands: `serve healthcheck`, `serve migrate`, `serve --behind-proxy`, and `serve backup --prune`, which drops from a backup what a purge or compact removed from the lake.
- **Device fleet on the dashboard.**
  - Each device shows its agent version, with a badge when it's behind or on a release with a known problem.
  - Devices can be revoked, unbound and given a profile from the dashboard.
  - Each device has a page with its reported inventory.
  - Agents gain `terva-lampi self-update`, which installs the lake's release after verifying it.
- **Agent profiles in the lake.** `serve profiles` imports, lists, sets and deletes profiles. The dashboard edits them, keeps each revision and rolls back. Edits reach connected agents within seconds.

#### Fixes

- The search index is reclaimed after each pass, and a new generation writes only changed rows.
- A Claude session keeps its own transcript as its head when it has subagents.
- Search marks only failed tool results as tool errors.
- The deduplication tile divides by the blobs actually on disk.
- Web UI 500s log their cause.
- A rotated IdP key verifies despite a stale JWKS fetch.
- Lake lock errors say to stop `serve` only when `serve` holds the lock.

**agent:claude-code/aa1afd80** at 2026-09-28T22:40:00Z

Supersedes the previous note's schema line. A real v0.1.2 lake reports schema 11, not 10; the source count missed migrate1. Main is at 15, adding device reports, profiles and device inventories after subagent heads. The corrected notes follow.

#### Upgrading from v0.1.2

- **The catalog migrates from schema 11 to 15.** The migrations add subagent heads, device reports, agent profiles with revisions, and device inventories. `serve` migrates when it starts, after copying `catalog.db` into `migration-backups/` in the lake directory. `serve migrate` does the same without starting the listener, and `serve migrate --check` reports what's pending. v0.1.2 can't open a migrated catalog, so rolling back means stopping `serve`, restoring that copy, deleting `catalog.db-wal` and `catalog.db-shm`, and then starting v0.1.2.
- **Upgrade the lake before the agents.** A new agent sends only the bytes appended to a transcript past 32 MiB, but only to a lake that advertises `large_tails`. Against a v0.1.2 lake it still works, sending whole chunks as before. A new lake also folds a grown file's old last chunk at ingest, so such transcripts no longer grow the lake quadratically between compactions.

#### New

- **A container image** at `ghcr.io/terva-sh/lampi`, for linux/amd64 and linux/arm64, with an SBOM and build provenance. It runs distroless as a non-root user, and a compose example puts Caddy in front of it. See `docs/container.md`. New commands: `serve healthcheck`, `serve migrate`, `serve --behind-proxy`, and `serve backup --prune`, which drops from a backup what a purge or compact removed from the lake.
- **Device fleet on the dashboard.**
  - Each device shows its agent version, with a badge when it's behind or on a release with a known problem.
  - Devices can be revoked, unbound and given a profile from the dashboard.
  - Each device has a page with its reported inventory.
  - Agents gain `terva-lampi self-update`, which installs the lake's release after verifying it.
- **Agent profiles in the lake.** `serve profiles` imports, lists, sets and deletes profiles. The dashboard edits them, keeps each revision and rolls back. Edits reach connected agents within seconds.

#### Fixes

- The search index is reclaimed after each pass, and a new generation writes only changed rows.
- A Claude session keeps its own transcript as its head when it has subagents.
- Search marks only failed tool results as tool errors.
- The deduplication tile divides by the blobs actually on disk.
- Web UI 500s log their cause.
- A rotated IdP key verifies despite a stale JWKS fetch.
- Lake lock errors say to stop `serve` only when `serve` holds the lock.

**agent:claude-code/aa1afd80** at 2026-09-28T22:44:17Z

Supersedes both earlier release-notes drafts, including the first draft's wrong schema 10 to 14 line: the canonical notes are in the implementation plan.

**agent:claude-code/aa1afd80** at 2026-09-28T22:57:23Z

### v0.2.0-rc1 (tag at e91ce6d, 2026-09-28)

- **GitHub release run 36494538739.** Archives published as a prerelease with 6 assets. The image was pushed as index `sha256:5c984d6f63d6…` with an SLSA provenance attestation naming `refs/tags/v0.2.0-rc1` and `.github/workflows/release.yml`. The SBOM manifests are attached.
- **The post-check failed.** `docker run --platform P IMAGE@INDEX_DIGEST` exits 125 with "cannot overwrite digest" in the runner's classic image store. That is a check defect; the image is fine. The fix runs each platform's own manifest digest, taken from the index with `docker buildx imagetools inspect --raw` and jq, and skips the two `unknown/unknown` attestation manifests.
- **Forgejo run 1073.** Both the release and the Buildah image build succeeded.
- **Checked by hand:**
  - amd64: `terva-lampi v0.2.0-rc1 (e91ce6d)`.
  - arm64: this host has no arm64 binfmt, so I extracted the binary instead. It's an ELF aarch64 static binary, GOARCH=arm64, stamped `v0.2.0-rc1`.
  - Upgrade: a lake seeded by the published v0.1.2 binary, served by the rc1 image under podman, migrated 11 → 15 after taking the backup. Both the v0.1.2 agent and the new agent synced appends. `serve fsck`: 5 entries, 0 bad.

**agent:claude-code/aa1afd80** at 2026-09-28T23:10:32Z

v0.2.0-rc2 at 3cadf74, with the #113 verify fix. GitHub release run 36495897095 succeeded in full, and its post-check now reports 'linux/amd64: terva-lampi v0.2.0-rc2 (3cadf74)' and 'linux/arm64: terva-lampi v0.2.0-rc2 (3cadf74)'. The Forgejo release and image build succeeded. The rc criterion is ticked on rc2, because rc1's image was good but its check was broken. v0.2.0 is tagged at 3cadf74, the same commit.

**agent:claude-code/aa1afd80** at 2026-09-28T23:16:51Z

### v0.2.0 run failed: goreleaser built it as 0.2.0-rc2

`v0.2.0` sits on 3cadf74, the same commit as `v0.2.0-rc2`. goreleaser chooses the current tag by git's version sort, where the `-rc2` suffix sorts after the release. So it built `v0.2.0` as `0.2.0-rc2`.

- **GitHub (run 36496558392).** goreleaser created no v0.2.0 release. It failed uploading the first archive, because the rc2 release already holds an asset of that name. The image job was skipped. The rc2 release is unchanged; its assets are still from 23:07.
- **Forgejo (run 1079).** It created a non-prerelease `v0.2.0` release holding archives named `terva-lampi_0.2.0-rc2_*`. The binaries inside report `v0.2.0 (3cadf74)`, because Go stamps the highest semver tag. The version check passed because it matches substrings, and `v0.2.0-rc2` contains `v0.2.0`. `install.sh` builds archive names from the version, so those names are wrong.
- **Fix (branch `ci/release-current-tag`).**
  - Both workflows set `GORELEASER_CURRENT_TAG: ${{ github.ref_name }}`, so goreleaser uses the tag that triggered the run.
  - All three version checks match the tag as a whole word.
  - A local goreleaser run at 3cadf74 with the pin built `terva-lampi_0.2.0_linux_amd64.tar.gz`.
- **Open.** The v0.2.0 tag still points at 3cadf74, which has the old workflow, so a re-run fails the same way. Fixing that needs the owner's decision: move the tag, or release v0.2.1. The Forgejo v0.2.0 release has to go in either case.

**agent:claude-code/aa1afd80** at 2026-09-28T23:18:14Z

### Decision (owner, 2026-09-28): move the v0.2.0 tag

The owner chose to rewrite the v0.2.0 tag rather than skip to v0.2.1. After #114 merges:

1. Delete the Forgejo `v0.2.0` release. Its archives are misnamed `0.2.0-rc2`.
2. Delete the `v0.2.0` tag on Forgejo and on GitHub, and locally.
3. Tag `v0.2.0` on the #114 merge commit and push it to both forges.

Why it's safe: the tag had existed for about 10 minutes when this was decided, and GitHub never published a release or an image for it. Forgejo's release is the only artifact.

Rejected: releasing v0.2.1 and leaving v0.2.0 as a tag with no GitHub release. That avoids the rewrite, but it leaves a hole in the version history and still needs the Forgejo release deleted.

The `v0.2.0-rc1` and `v0.2.0-rc2` tags and releases stay as they are.

**agent:claude-code/aa1afd80** at 2026-09-28T23:29:35Z

### v0.2.0 published (tag at 3f71211)

- **GitHub release run 36497577376 succeeded.**
  - Five archives named `terva-lampi_0.2.0_*`, plus `checksums.txt`, published as a full release, not a prerelease.
  - The binary check reported `terva-lampi v0.2.0 (3f71211d33db)`.
  - The image was pushed as `0.2.0`, `0.2`, `0`, `latest` and `sha-3f71211`, with an SBOM and provenance.
  - The exact per-platform check reported `v0.2.0 (3f71211)` for both linux/amd64 and linux/arm64.
- **Forgejo.** The release and Buildah image build at 3f71211 succeeded. The release is not a prerelease and has the same five `0.2.0` archives.
- **Notes.** The canonical notes from this ticket's plan now sit above the generated body on both releases.
- **install.sh.** `install.sh` fetched from the v0.2.0 tag on raw.githubusercontent.com installed a binary reporting `terva-lampi v0.2.0 (3f71211d33db)`.
- **Tag history.** The v0.2.0 tag was moved from 3cadf74 to 3f71211 as the owner authorized, and the misnamed Forgejo release was deleted first. `v0.2.0-rc1` and `v0.2.0-rc2` remain as prereleases.

Remaining: the owner sets the `lampi` package public in GHCR, and then the image is checked to pull without a login.

**agent:claude-code/aa1afd80** at 2026-09-28T23:29:35Z

in-progress to blocked: Waiting on the owner to set the GHCR lampi package public; then check an anonymous pull of ghcr.io/terva-sh/lampi:0.2.0.
