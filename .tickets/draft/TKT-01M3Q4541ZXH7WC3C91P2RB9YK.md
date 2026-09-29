---
schema: 3
id: TKT-01M3Q4541ZXH7WC3C91P2RB9YK
title: "CI Build Image: Go runtime file-name collision in the build stage"
type: chore
status: draft
status_reason: null
priority: low
due_on: null
labels:
  - area/ci
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-29T17:42:14Z
updated_at: 2026-09-29T17:42:14Z
created_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
extensions: {}
---

## Description

Forgejo CI run 1526 (PR #155, head cf8cdd8, 2026-09-29), job "Build
Image", failed in the amd64 stage at `go build ./cmd/terva-lampi`:

    imports runtime from value.go: case-insensitive file name collision:
    "sys_ppc64x.go" and "sys_ppc64x.go"

The collision is inside the Go toolchain's own runtime sources in the
pinned `golang@sha256:69a7b978…` build image, with the same file name
twice, so it points at the runner's buildah storage (a corrupted or
doubly-applied layer) rather than the repository. The same Dockerfile
built for the same branch an hour earlier (run for a701f4a) and on main.
Lint and Test passed in the same run.

### To find out

- Whether it repeats, and on which runner.
- Whether buildah's layer cache on that runner needs pruning.
