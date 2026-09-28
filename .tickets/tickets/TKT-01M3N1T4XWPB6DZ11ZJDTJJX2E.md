---
schema: 3
id: TKT-01M3N1T4XWPB6DZ11ZJDTJJX2E
title: "Release v0.2.0: notes, rc1, and the first GHCR image"
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
updated_at: 2026-09-28T22:38:34Z
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
   - upgrade a schema-10 lake from a v0.1.2 image.
5. An owner sets the `lampi` package public in GHCR, which closes TKT-01M3MC0RE.
6. Tag `v0.2.0`, attach the notes to both releases, then upgrade the hosted lake before the agents (TKT-01M3FP11A, "Onboarding rollout: upgrade the hosted lake and register machines").

The release workflow runs `go test ./...` before it publishes. Three tests are known to be flaky under load: TKT-01M3MKF5, TKT-01M3MJDS and TKT-01M3MSE6. If one fails, re-run the job; a flake is not a reason to change the tag.

## Acceptance criteria

- [ ] v0.2.0-rc1 published archives and a two-platform image that passed the checks
- [ ] The GHCR package is public and pulls without a login
- [ ] v0.2.0 is tagged on both forges with the notes attached
- [ ] Release notes state the schema 10 to 14 migration, its rollback, and lake-before-agents
- [ ] A v0.1.2 lake upgraded to the rc image and its agents still sync

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
