---
schema: 3
id: TKT-01M44DVPS495GHD09WP2V8GZJH
title: "Deps: bump klauspost/compress to v1.20.1"
type: chore
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area/ci
  - area/cas
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim:
  actor: agent:claude-code/9078ac3f
  branch: deps/compress
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-268e1246
  commit: 431d9b7c9b1d488a56585a0d6194d5be039d6da9
  session: null
  claimed_at: 2026-10-04T21:46:37Z
  expires_at: null
archive: null
created_at: 2026-10-04T21:41:59Z
updated_at: 2026-10-04T21:46:37Z
created_by:
  id: agent:claude-code/9078ac3f
  name: ""
updated_by:
  id: agent:claude-code/9078ac3f
  name: ""
extensions: {}
---

## Description

govulncheck run on 2026-10-04 reported GO-2026-5841 in `github.com/klauspost/compress` v1.17.9 and found it unreachable from lampi's code. The current release is v1.20.1.

lampi imports `klauspost/compress/zstd` directly in `internal/cas`, `internal/archive` and `internal/normalize` (events files), and `parquet-go` uses the module too. A new zstd encoder may write different bytes for the same input. Before merging, check that nothing compares compressed bytes, such as a golden file, a hash taken over compressed data, or a test fixture. Also check that data written by v1.17.9 still reads.

This one goes on its own, apart from the `golang.org/x` bump, because it is the one dependency here whose output lands on disk.

## Acceptance criteria

- [ ] go.mod requires github.com/klauspost/compress v1.20.1 or newer.
- [ ] govulncheck ./... reports no finding in klauspost/compress.
- [ ] Nothing in lampi compares zstd output bytes, and a store written with v1.17.9 still reads; the PR says how this was checked.
- [ ] GOFLAGS=-mod=mod just ci passes.

## Implementation plan

go get github.com/klauspost/compress@v1.20.1 and tidy. Confirm nothing compares compressed bytes (CAS keys are digests of the uncompressed bytes; the events-file index is written with the file). Write a CAS store and an events file with main's code at v1.17.9 and read them back with the bumped code, through temporary tests that are not committed. Rerun govulncheck and the full gate; compare internal/api, internal/cas and internal/normalize test times before and after, since zstd cost shows up as CI wall-clock time.
