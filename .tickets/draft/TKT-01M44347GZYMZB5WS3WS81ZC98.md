---
schema: 3
id: TKT-01M44347GZYMZB5WS3WS81ZC98
title: go.mod lists zeebo/blake3 as indirect; -mod=mod rewrites it
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
created_at: 2026-10-04T18:34:24Z
updated_at: 2026-10-04T18:34:33Z
created_by:
  id: agent:claude-code/9078ac3f
  name: ""
updated_by:
  id: agent:claude-code/9078ac3f
  name: ""
extensions: {}
---

## Description

`internal/adapter/grok/cwd.go` imports `github.com/zeebo/blake3` directly (added in #79, Grok Build discover and watch adapter), but `go.mod` still lists it under `// indirect`. Any `go` command run with `GOFLAGS=-mod=mod`, which overrides a workstation `GOFLAGS=-trimpath` that breaks some tests, rewrites `go.mod` to move it into the direct block. That leaves an unrelated `go.mod` change in every working tree that runs the gate that way.

Fix: run `go mod tidy` and commit the result. Check that CI does not need anything else from the tidy.
