---
schema: 3
id: TKT-01M3MC0RNDRXWT2VDGG26A6VW5
title: "Release CI: publish the multi-arch lake image to GHCR on v* tags"
type: task
status: ready
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
references: []
claim: null
archive: null
created_at: 2026-09-28T16:01:57Z
updated_at: 2026-09-28T16:03:33Z
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
- [ ] The Forgejo release builds the image without publishing to GHCR
- [ ] Published images carry an SBOM and build provenance
- [ ] A post-publish check confirms each platform's --version carries the tag
- [ ] Pull requests build both platforms without pushing
