---
schema: 3
id: TKT-01M3MC0RNDRXWT2VDGG26A6VW5
title: "Release CI: publish the multi-arch lake image to GHCR on v* tags"
type: task
status: in-progress
status_reason: null
priority: high
due_on: null
labels:
  - area/ci
  - area/ops
assignees: []
milestone: null
parent: TKT-01M3MC023P4A5H7PTF662QSSM8
origin: null
dependencies:
  - TKT-01M3MC0QVMMW0TQ6RGAYDZEF82
  - TKT-01M3MC0RET5Y2PKD4REW649Q8M
blocks_on: none
references:
  - ref: pr:forgejo/terva-sh/lampi#97
    path: null
claim:
  actor: agent:claude-code/aa1afd80
  branch: self-host/ghcr-ci
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-aa1afd80
  commit: ae9c2e2674773db9a17050e6f6229b6522809d11
  session: null
  claimed_at: 2026-09-28T19:15:05Z
  expires_at: null
archive: null
created_at: 2026-09-28T16:01:57Z
updated_at: 2026-09-28T19:24:45Z
created_by:
  id: agent:claude-code/aa1afd80
  name: ""
updated_by:
  id: agent:claude-code/aa1afd80
  name: ""
extensions: {}
---

## Description

Publish the lake image from CI on every release, for `linux/amd64` and `linux/arm64`, and build it (without pushing) on every change so a broken Containerfile shows up before a tag.

### Release

- **Trigger and guard.** A `v*` tag in `.github/workflows/release.yml`, guarded to github.com the same way the goreleaser job is. The Forgejo twin builds the image but doesn't publish to GHCR.
- **Tool.** Either goreleaser's Docker support, which reuses the binaries it already builds and publishes one multi-arch manifest, or a separate `docker/build-push-action` job with buildx. Goreleaser keeps the version stamping and the archive binaries identical. buildx keeps the Containerfile usable on its own. Pick one and record why.
- **Identity.** Log in to GHCR with `GITHUB_TOKEN` and `permissions: packages: write` (plus `id-token: write` and `attestations: write` if signing).
- **Output.** A multi-arch manifest list with the tags from the registry ticket.
- **Supply chain.** An SBOM and SLSA build provenance attached to the image (`actions/attest-build-provenance`, or buildx `--sbom --provenance`). Keyless cosign signing if it's cheap to add. The docs say how to verify.
- **Post-check.** Pull the published digest for each platform and run `--version` to confirm it carries the tag, like the existing archive check. arm64 can run on an arm64 runner or under QEMU, since this is a check, not the build.

### On pull requests and main

- Build both platforms without pushing, in `ci.yml` or a separate workflow, only when the Containerfile or Go code changed if that saves time.
- Run the container smoke tests (`internal/synthetic/container`, build tag `synthetic_container`) against the built amd64 image. Either run them against the production image or keep `e2e/Dockerfile` as its own target, and record which.
- Pin the Actions and base images by digest, and let Renovate or Dependabot update them.

Check `terva-review`'s size limit: keep the workflow change in its own PR.

## Acceptance criteria

- [ ] A v* tag on GitHub publishes one amd64+arm64 manifest with the agreed tags
- [x] The Forgejo release builds the image without publishing to GHCR
- [ ] Published images carry an SBOM and build provenance
- [ ] A post-publish check confirms each platform's --version carries the tag
- [x] Pull requests build both platforms without pushing

## Implementation plan

### Decisions

**The image comes from buildx and the Dockerfile, not goreleaser's Docker support.** The Dockerfile is what `just image` builds, what the compose guide runs, and what the Forgejo job proves, so the release image comes from the same file. Goreleaser would copy its own binaries into a second, separate image definition.

**The version stamp is the full tag with its `v`.** An archive's `--version` reports `v0.2.0` from go build's VCS data, and self-update compares the lake's version with the agent's. The image passes `VERSION=${{ github.ref_name }}`, so both name the same release.

**Tags** are the ones decided on TKT-01M3MC0RE (GHCR: set up the terva-sh container package for the lake image), produced with `docker/metadata-action`: semver `{{version}}`, `{{major}}.{{minor}}`, `{{major}}`, `latest=auto`, and `sha-<short>`. For a pre-release, the action writes only `{{version}}`.

**Forgejo builds with Buildah**, following `Sothr-Containers/*/.forgejo/workflows/build.yml`: the `library/buildah` image with `/dev/fuse`. It builds a two-platform manifest and runs the amd64 binary with `buildah run`, since the image has no shell. The image needs the fully qualified name `localhost/terva-lampi:ci`, because Buildah won't resolve a short name without a registry configuration.

**Actions are pinned by tag, not digest,** matching the repository's existing workflows. Nothing here updates digests automatically, so digest pins would go stale silently.

### Steps

1. `.github/workflows/release.yml`: an `image` job, after `goreleaser`, with QEMU (for the arm64 post-check only), buildx, a GHCR login with `GITHUB_TOKEN`, metadata, build and push with `provenance: mode=max` and `sbom: true`, `attest-build-provenance`, and a `--version` check on both platforms.
2. `.github/workflows/ci.yml`: an `image` job that builds both platforms with `outputs: type=cacheonly`.
3. `.forgejo/workflows/ci.yml` and `release.yml`: the Buildah build and the amd64 run.
4. Docs: `docs/development.md#releases`, and a "Verify the image" section in `docs/container.md` using `gh attestation verify`.

## Notes

**agent:claude-code/aa1afd80** at 2026-09-28T19:15:05Z

Checked before push: actionlint passes on .github/workflows, and every workflow file parses as YAML. I ran the Forgejo job's Buildah commands locally (buildah build --format docker --platform linux/amd64,linux/arm64 --manifest localhost/terva-lampi:ci, then buildah run on the amd64 binary) against c43f485. The build made an amd64+arm64 manifest, and --version and serve healthcheck --help ran. The same commands on current main fail to compile, because main is broken by #93 and #88 (internal/web/devices.go calls pageError and fail without the request), and PR #96 from another session fixes that. The GitHub release job can only be proven by a real v* tag; its criteria stay open until one is pushed.

**agent:claude-code/aa1afd80** at 2026-09-28T19:24:45Z

On PR #97, after #96 fixed main, Forgejo's 'Build Image' job passed on the real runner in 4m1s: Buildah with /dev/fuse pulled the pinned bases and built both platforms. Criteria 2 and 5 are ticked on that. The GitHub CI image job runs only on the GitHub mirror, and the release job only on a tag; 1, 3 and 4 wait for one.
