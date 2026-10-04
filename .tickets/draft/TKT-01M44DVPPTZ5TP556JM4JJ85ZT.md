---
schema: 3
id: TKT-01M44DVPPTZ5TP556JM4JJ85ZT
title: "Deps: bump golang.org/x/crypto and x/sys to current releases"
type: chore
status: draft
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
references: []
claim: null
archive: null
created_at: 2026-10-04T21:41:59Z
updated_at: 2026-10-04T21:41:59Z
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

- [ ] go.mod requires golang.org/x/crypto v0.57.0 and golang.org/x/sys v0.48.0, or newer.
- [ ] govulncheck ./... reports no finding in golang.org/x/crypto.
- [ ] GOFLAGS=-mod=mod just ci passes and leaves go.mod unchanged.
