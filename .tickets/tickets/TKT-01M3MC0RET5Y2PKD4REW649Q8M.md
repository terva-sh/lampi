---
schema: 3
id: TKT-01M3MC0RET5Y2PKD4REW649Q8M
title: "GHCR: set up the terva-sh container package for the lake image"
type: chore
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/ops
  - area/ci
assignees: []
milestone: v0.2.0
parent: TKT-01M3MC023P4A5H7PTF662QSSM8
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-28T16:01:57Z
updated_at: 2026-09-28T23:29:36Z
created_by:
  id: agent:claude-code/aa1afd80
  name: ""
updated_by:
  id: agent:claude-code/aa1afd80
  name: ""
extensions: {}
---

## Description

Set up the GitHub Container Registry package for the lake image, owned by the `terva-sh` organization and linked to `github.com/terva-sh/lampi`.

This changes the settings of a remote organization and package, so AGENTS.md requires a ticket to authorize it. This ticket is that authorization once it's promoted. A person with admin rights on `terva-sh` does the settings steps. An agent can prepare and check them but shouldn't change organization settings on its own.

### Decisions to record here

- **Image name.** `ghcr.io/terva-sh/lampi` (matches the repository) or `ghcr.io/terva-sh/terva-lampi` (matches the binary and the archive names). Pick one and use it everywhere.
- **Tags.** Suggested: `X.Y.Z`, `X.Y`, `X` and `latest` on a stable `v*` tag; nothing floating on a pre-release; and a `sha-<short>` tag for every published build. `latest` moves only on a stable release. The docs tell operators to pin `X.Y.Z` or a digest, because a minor release may migrate the catalog.
- **Visibility.** Public, so `docker pull` works without logging in.
- **Retention.** Delete untagged manifests after a period. Keep every release tag.
- **Forgejo.** Whether the internal Forgejo registry on `git.local.sothr.com` should get the image too, or GitHub only. Default to GitHub only, following the release-archive setup, where Forgejo builds but GitHub is the public source.

### Settings

- The package is linked to the repository, either through the `org.opencontainers.image.source` label on the first push or by hand. The repository's Actions have write access to it, so the release workflow publishes with `GITHUB_TOKEN` (`packages: write`) and no personal token.
- Visibility is set to public after the first push, because GHCR creates new packages as private.

## Acceptance criteria

- [x] The image name, tag scheme, visibility, retention and Forgejo decision are recorded
- [ ] The GHCR package is public and linked to terva-sh/lampi
- [x] The repository's Actions can push to the package with GITHUB_TOKEN

## Notes

**agent:claude-code/aa1afd80** at 2026-09-28T17:47:08Z

Image name decided by the owner on 2026-09-28: ghcr.io/terva-sh/lampi, matching the repository. The binary and archives keep the name terva-lampi. The tag scheme, visibility, retention, and Forgejo decisions are still open.

**agent:claude-code/aa1afd80** at 2026-09-28T19:08:40Z

### Decisions (owner, 2026-09-28)

- **Image:** `ghcr.io/terva-sh/lampi`, as recorded earlier.
- **Tags.**
  - A stable `vX.Y.Z` publishes `X.Y.Z`, `X.Y`, `X`, and `latest`.
  - A pre-release such as `v0.3.0-rc1` publishes only its exact tag, never `latest` or the short tags.
  - Every publish also gets `sha-<short>`.
  - The docs keep telling operators to pin `X.Y.Z` or a digest.
- **Forgejo:** GitHub only. The Forgejo release job builds both platforms to prove the Dockerfile, and pushes nowhere. This matches the archives, where GitHub is the public source.
- **Retention:** keep everything for now. Releases are infrequent and an image is small. Revisit if the package grows.

### What is left here

The first push from `.github/workflows/release.yml`, with `GITHUB_TOKEN` and `packages: write`, creates the package. The image's `org.opencontainers.image.source` label links it to `terva-sh/lampi`, so the repository's Actions keep write access. GHCR creates the package private. After that first push, someone with admin on `terva-sh` sets it to public. That is the one step for a person.

**agent:claude-code/aa1afd80** at 2026-09-28T23:29:36Z

Actions push with GITHUB_TOKEN works: v0.2.0-rc1, v0.2.0-rc2 and v0.2.0 all pushed to ghcr.io/terva-sh/lampi. The package still needs setting public by an owner.
