---
schema: 3
id: TKT-01M44347GZYMZB5WS3WS81ZC98
title: go.mod lists zeebo/blake3 as indirect; -mod=mod rewrites it
type: chore
status: done
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
references:
  - ref: pr:193
    path: null
claim: null
archive: null
created_at: 2026-10-04T18:34:24Z
updated_at: 2026-10-04T22:13:34Z
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

## Implementation plan

Done by the go mod tidy in TKT-01M44DVPPTZ5TP556JM4JJ85ZT (Deps: bump golang.org/x/crypto and x/sys), in the same PR.

## Summary

Fixed in PR 193 by the go mod tidy that came with TKT-01M44DVPPT (Deps: bump golang.org/x/crypto and x/sys): zeebo/blake3 is in go.mod's direct block. A full GOFLAGS=-mod=mod gate leaves go.mod unchanged, on the branch and on main after the merge. CI needed nothing else from the tidy.
