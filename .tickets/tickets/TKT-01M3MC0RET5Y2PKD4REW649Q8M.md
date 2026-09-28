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
milestone: null
parent: TKT-01M3MC023P4A5H7PTF662QSSM8
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-28T16:01:57Z
updated_at: 2026-09-28T17:47:08Z
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

- [ ] The image name, tag scheme, visibility, retention and Forgejo decision are recorded
- [ ] The GHCR package is public and linked to terva-sh/lampi
- [ ] The repository's Actions can push to the package with GITHUB_TOKEN

## Notes

**agent:claude-code/aa1afd80** at 2026-09-28T17:47:08Z

Image name decided by the owner on 2026-09-28: ghcr.io/terva-sh/lampi, matching the repository. The binary and archives keep the name terva-lampi. The tag scheme, visibility, retention, and Forgejo decisions are still open.
