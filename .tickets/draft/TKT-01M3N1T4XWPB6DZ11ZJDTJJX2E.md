---
schema: 3
id: TKT-01M3N1T4XWPB6DZ11ZJDTJJX2E
title: "Release v0.2.0: notes, rc1, and the first GHCR image"
type: task
status: draft
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
dependencies:
  - TKT-01M3MC0RNDRXWT2VDGG26A6VW5
  - TKT-01M3MC0RET5Y2PKD4REW649Q8M
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-28T22:22:49Z
updated_at: 2026-09-28T22:22:53Z
created_by:
  id: agent:claude-code/aa1afd80
  name: ""
updated_by:
  id: agent:claude-code/aa1afd80
  name: ""
extensions: {}
---

## Description

Cut v0.2.0, the first release since v0.1.3. It is also the first release that publishes the lake image to `ghcr.io/terva-sh/lampi`. The process is in `docs/development.md` under "Releases".

### Order

1. Merge and sync (`just sync-github --yes`), so the commit is on both mains.
2. Write the release notes (below).
3. Tag `v0.2.0-rc1` on that commit and push the tag to both forges. A hyphenated tag publishes a prerelease, and the image gets only `0.2.0-rc1` and `sha-<short>`, never `latest`. This is the first run of the image publish, so it proves TKT-01M3MC0RN (Release CI: publish the multi-arch lake image to GHCR on v* tags) without moving `latest`.
4. Check the rc:
   - `docker pull` the image and run `--version` on amd64 and arm64;
   - `gh attestation verify` as in `docs/container.md`;
   - bring up the compose example on the rc and upgrade a schema-10 lake from a v0.1.3 image.
5. An owner sets the `lampi` package public in GHCR (TKT-01M3MC0RE, "GHCR: set up the terva-sh container package for the lake image").
6. Tag `v0.2.0`, then upgrade the hosted lake before the agents (TKT-01M3FP11A, "Onboarding rollout: upgrade the hosted lake and register machines").

The release workflow runs `go test ./...` before it publishes. Three tests are known to be flaky under load: TKT-01M3MKF5, TKT-01M3MJDS and TKT-01M3MSE6. If one fails, re-run the job; a flake is not a reason to change the tag.

### Release notes must say

- **Catalog migration: schema 10 to 13.** The migrations add agent profiles, profile revisions and each device's agent report. `serve` migrates at start after copying the catalog to `migration-backups/`. A v0.1.x binary cannot open the result, so rolling back means restoring that copy (`serve migrate --help`, `docs/container.md`).
- **Upgrade the lake before the agents.** Agents send only the appended bytes of a file past 32 MiB, but only to a lake that advertises `large_tails` (#62, #81). A lake on v0.1.3 still works with new agents; they send whole chunks, as before.
- **New: a container image and a compose setup behind Caddy** (#77, #91, #97), plus `serve migrate`, `serve healthcheck`, `serve backup --prune` and `--behind-proxy`.
- **New: device fleet.** The dashboard shows each device's agent version with behind and urgent badges, and devices can be revoked, unbound and given a profile there (#85, #88, #90). Agents also get `terva-lampi self-update` (#80).
- **New: profiles.** Agent profiles are stored in the lake and edited from the dashboard with revisions and rollback (#69 to #71, #78, #100 to #102). Edits reach agents within seconds.
- **New: device inventory.** Agents report their inventory, and the dashboard has a page per device (#106 to #109).
- **Storage and search fixes:** #62, #63, #64, #73, #81, #87 and #94.

## Acceptance criteria

- [ ] Release notes state the schema 10 to 13 migration, its rollback, and lake-before-agents
- [ ] v0.2.0-rc1 published archives and a two-platform image that passed the checks
- [ ] A v0.1.3 lake upgraded to the rc image and its agents still sync
- [ ] The GHCR package is public and pulls without a login
- [ ] v0.2.0 is tagged on both forges with the notes attached
