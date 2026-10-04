---
schema: 3
id: TKT-01M44DVPS495GHD09WP2V8GZJH
title: "Deps: bump klauspost/compress to v1.20.1"
type: chore
status: done
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
references:
  - ref: pr:194
    path: null
claim: null
archive: null
created_at: 2026-10-04T21:41:59Z
updated_at: 2026-10-04T22:13:35Z
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

- [x] go.mod requires github.com/klauspost/compress v1.20.1 or newer.
- [x] govulncheck ./... reports no finding in klauspost/compress.
- [x] Nothing in lampi compares zstd output bytes, and a store written with v1.17.9 still reads; the PR says how this was checked.
- [x] GOFLAGS=-mod=mod just ci passes.

## Implementation plan

go get github.com/klauspost/compress@v1.20.1 and tidy. Confirm nothing compares compressed bytes (CAS keys are digests of the uncompressed bytes; the events-file index is written with the file). Write a CAS store and an events file with main's code at v1.17.9 and read them back with the bumped code, through temporary tests that are not committed. Rerun govulncheck and the full gate; compare internal/api, internal/cas and internal/normalize test times before and after, since zstd cost shows up as CI wall-clock time.

## Notes

**agent:claude-code/9078ac3f** at 2026-10-04T21:49:24Z

Checked: nothing in lampi compares zstd output. CAS keys are sha256 of the bytes before compression (internal/cas/zstd.go), and an events file's frame index is written with the file. Cross-version check through two temporary tests, not committed: one wrote a CAS store (16 objects, 0 B to 3 MiB, random and repetitive, either side of the 256-byte content-size cutoff) and a 5,000-event indexed events file with one version; the other read them with cas.Read, cas.Size, ReadEventsFile and EventsFile.From at five positions. Written by v1.17.9 and read by v1.20.1, and the reverse, both byte for byte. govulncheck: GO-2026-5841 is gone; the only finding left is GO-2026-5932 (x/crypto/openpgp advisory, no fix, not imported). Full gate passed. Test times, v1.17.9 then v1.20.1: internal/api 9.1s, 8.5s; internal/cas 2.9s, 2.9s; internal/normalize 0.5s, 0.3s.

## Summary

Landed in PR 194: `klauspost/compress` v1.17.9 → v1.20.1. govulncheck no longer reports GO-2026-5841.

Nothing in lampi compares zstd output bytes. A CAS store and an indexed events file written by either version read back byte for byte with the other, checked through temporary tests that were not committed. The note on this ticket gives the details. Test times stayed flat: `internal/api` 9.1s → 8.5s, `internal/cas` 2.9s → 2.9s, `internal/normalize` 0.5s → 0.3s.
