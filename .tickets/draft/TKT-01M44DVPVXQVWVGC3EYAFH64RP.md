---
schema: 3
id: TKT-01M44DVPVXQVWVGC3EYAFH64RP
title: "MCP: serve the lake endpoint with the official Go SDK"
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/search
  - area/server
assignees: []
milestone: null
parent: TKT-01M3FPP3H592T31Y2M3N347CPB
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

Serve `/api/read/v1/mcp` with the official Go SDK, `github.com/modelcontextprotocol/go-sdk`, instead of the hand-written JSON-RPC and protocol layer from TKT-01M3FPWCF (MCP: serve recall tools over the shared query layer). The tools, their arguments, results, error bodies and hints, the token check, the scope, and the audit stay lampi's code. Only the protocol layer moves to the SDK.

This answers the question that TKT-01M445H1 (MCP follow-ups: bay-token tests, bridge limits, audit rate) left for whoever revisits the SDK.

### What was measured, 2026-10-04

The SDK's cost was measured against `main`, after `go mod tidy`, on linux/amd64.

| | official go-sdk v1.8.0 | mark3labs mcp-go v1.1.1 |
|---|---|---|
| `terva-lampi` binary | +1.74 MB (+7.3%) | +1.09 MB (+4.6%) |
| Modules linked into the binary | +7 | +6 |
| `go.sum` lines | +16 | +32 |
| Build list | +8 | +15 |
| Existing dependencies bumped | none | none |

The official SDK links `google/jsonschema-go`, `segmentio/asm`, `segmentio/encoding`, `yosida95/uritemplate/v3`, `golang.org/x/sync` and `golang.org/x/time`. govulncheck reports nothing in any of them. Both SDKs serve 2026-07-28.

A prototype port ran the unchanged tests against the SDK, along with a 55-case probe that compared each request's answer from both servers. The probe showed that the hand-written server misses four 2026-07-28 rules that the SDK follows:

- `ping` is removed in 2026-07-28. Ours answered it. The SDK answers 404 with -32601.
- Every request's `_meta` carries `io.modelcontextprotocol/clientCapabilities`. Ours did not require it.
- `tools/list` results carry `ttlMs` and `cacheScope`. Ours had neither.
- Each result's `_meta` should carry `io.modelcontextprotocol/serverInfo`. Ours put it on `server/discover` only.

The SDK misses one rule that ours follows: it compares `Mcp-Name` to the body without decoding the `=?base64?…?=` form, which the spec says a server MUST decode.

### Alternatives

- **Keep the hand-written server and fix the four gaps.** Each fix is small, but the hand-written layer is about 200 lines that every protocol revision makes us revisit. Within weeks of 2026-07-28 it had already fallen behind in four places, and six of the eight review findings on TKT-01M3FPWCF were protocol corners in that code. It lost on upkeep.
- **mark3labs/mcp-go.** It has the smaller binary cost but twice the `go.sum` and build-list growth, and it is third-party. It lost to the reference SDK.
- **The official SDK.** It is maintained with the specification and has caught up with each revision so far. The port is smaller: `internal/web/mcp.go` went from 551 lines to about 400 in the prototype, most of what is left is lampi's tool code, and the tests prove that code is unchanged. Chosen.

### Kept as lampi's own, deliberately

- Bearer auth stays in `tokenFor`, in front of the SDK. The SDK's `auth` package would link `golang-jwt/jwt/v5` next to the `go-jose` that `go-oidc` already brings.
- Arguments are validated once, by the query layer's own parsers. The SDK's typed `AddTool[In, Out]` would put a second validator in front of them, whose errors bypass the `invalid_request` body and hint, and whose refusals would skip the audit.
- The Origin check stays ours, matched against `base_url`. The SDK's localhost protection is off because the lake runs behind a proxy, so its Host header is the lake's public name.
- The stdio bridge, `terva-lampi mcp`, stays a forwarder. An SDK client-and-server pair would negotiate one version with the agent and another with the lake, would snapshot the tool list at start, and would need the lake reachable before the agent's first message.
- The open-route rate limiter in `internal/api/identity.go` and the channel semaphores in `internal/api/server.go` and `internal/webauth` stay as they are, even though `x/time/rate` and `x/sync` now ship in the binary. `rate.Limiter` moves its clock back when wall time steps back, and refills that gap again on the next call. Ours does not. A per-token MCP rate limit (TKT-01M445H1) may use `x/time/rate`.

## Acceptance criteria

- [ ] /api/read/v1/mcp is served by the go-sdk mcp.Server through its streamable HTTP handler, stateless, answering in JSON.
- [ ] TestMCPToolsMatchTheWebAPI and TestMCPReadsWithinTheTokenScope pass unchanged: same tools, results, errors, hints, scope and audit lines, an unknown tool's included.
- [ ] A 2026-07-28 request is answered as that revision says: ping is 404, clientCapabilities is required, tools/list carries ttlMs and cacheScope, and results carry serverInfo.
- [ ] A test drives the endpoint with the SDK's own client.
- [ ] terva-lampi mcp works against the new endpoint and its tests pass.
- [ ] docs/web-api.md says where lampi deliberately departs from the SDK's defaults or from the spec, and why.
