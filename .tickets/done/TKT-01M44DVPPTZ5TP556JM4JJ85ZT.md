---
schema: 3
id: TKT-01M44DVPPTZ5TP556JM4JJ85ZT
title: "Deps: bump golang.org/x/crypto and x/sys to current releases"
type: chore
status: done
status_reason: null
priority: normal
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
created_at: 2026-10-04T21:41:59Z
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

govulncheck run on 2026-10-04, while measuring the MCP SDKs, reported 17 vulnerabilities in `golang.org/x/crypto` v0.45.0, which lampi requires indirectly. govulncheck found none of them reachable from lampi's code, so this is hygiene, not an incident. v0.45.0 is twelve releases behind the current v0.57.0.

The `golang.org/x` modules are released together. `x/oauth2` is current (v0.37.0) and `x/sys` is one release behind (v0.47.0, current v0.48.0). Bring `x/crypto` and `x/sys` to the current releases so the family moves together. The MCP SDK adds `x/sync` and `x/time` at the versions it asks for. Bump those only together with the SDK.

The same change runs `go mod tidy`, which moves `zeebo/blake3` into the direct block (TKT-01M44347GZ, go.mod lists zeebo/blake3 as indirect).

### Not in scope

`klauspost/compress` has the one other govulncheck finding. It is a direct dependency that the CAS, archive and events-file code use for zstd, and that `parquet-go` uses too, so it gets its own ticket and PR.

## Acceptance criteria

- [x] go.mod requires golang.org/x/crypto v0.57.0 and golang.org/x/sys v0.48.0, or newer.
- [ ] govulncheck ./... reports no finding in golang.org/x/crypto.
- [x] GOFLAGS=-mod=mod just ci passes and leaves go.mod unchanged.

## Implementation plan

go get golang.org/x/crypto@v0.57.0 golang.org/x/sys@v0.48.0, then go mod tidy, which also moves zeebo/blake3 into the direct block (TKT-01M44347GZ). Check what else the bump pulls in, rerun govulncheck, run the full gate with GOFLAGS=-mod=mod and confirm go.mod is unchanged afterwards.

## Notes

**agent:claude-code/9078ac3f** at 2026-10-04T21:44:27Z

Criterion 2 is left open. After the bump, govulncheck v1.8.0 on ./... still reports one module-level finding in x/crypto: GO-2026-5932, an advisory that golang.org/x/crypto/openpgp is unmaintained, which has no fixed version. No lampi package imports openpgp (go list -deps ./... lists chacha20, chacha20poly1305, curve25519, hkdf, pbkdf2 and scrypt from x/crypto). The sixteen findings that had fixes are gone. The criterion as written cannot be met by any x/crypto release, so it stays unticked rather than reworded after the fact. The full gate passed with GOFLAGS=-mod=mod and left go.mod unchanged.

## Summary

Landed in PR 193.

- `golang.org/x/crypto` v0.45.0 → v0.57.0 and `golang.org/x/sys` v0.47.0 → v0.48.0, with no other module moved.
- `go mod tidy` moved `zeebo/blake3` into the direct block (TKT-01M44347GZ).

govulncheck no longer reports the sixteen x/crypto findings that had fixes. It still reports GO-2026-5932, an advisory that `x/crypto/openpgp` is unmaintained. That advisory has no fixed version, and lampi imports no openpgp package. Criterion 2 is left unticked for that reason.

The full gate passed on the branch, and again on the branch merged onto `main`, with `GOFLAGS=-mod=mod`. It left `go.mod` unchanged.
