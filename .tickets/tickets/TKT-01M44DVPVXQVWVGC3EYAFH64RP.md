---
schema: 3
id: TKT-01M44DVPVXQVWVGC3EYAFH64RP
title: "MCP: serve the lake endpoint with the official Go SDK"
type: task
status: in-progress
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
claim:
  actor: agent:claude-code/9078ac3f
  branch: mcp/go-sdk
  worktree: /home/sothr/.cache/agent-scratch/lampi/sdk-port-CTOO/sdk
  commit: 603227a9b072603af847aa1a203b23f81087a8ed
  session: null
  claimed_at: 2026-10-04T21:50:10Z
  expires_at: null
archive: null
created_at: 2026-10-04T21:41:59Z
updated_at: 2026-10-04T21:50:10Z
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

## Implementation plan

One PR, stacked on #193 until it merges.

### internal/web/mcp.go

- One `mcp.Server` built in `mcpRoutes`. Its options are `Instructions`, and `Capabilities` set to tools only, with no `listChanged`, because the tool list is fixed. The SDK's default also adds `logging`, which 2026-07-28 deprecates.
- `mcp.NewStreamableHTTPHandler` with `Stateless`, `JSONResponse`, `MaxRequestBodyBytes: mcpBodyMax` and `DisableLocalhostProtection`.
- lampi's gate goes in front of it, on `POST /api/read/v1/mcp`:
  1. `tokenFor` with events:read.
  2. The Origin check against `base_url`: 403 with a JSON-RPC error that has no id.
  3. `Cache-Control: no-store`.
  4. A decoded `Mcp-Name` when it is in the base64 form. The SDK compares the header to the body as sent, and the transport says a server decodes it first.
  5. The token and request, put in the request context for the tool handler.
- The 405 answer for GET and DELETE stays the lake's own JSON route.
- A receiving middleware audits every `tools/call` before the SDK looks the tool up. This one place covers an unknown tool, which the SDK refuses before any tool handler runs.
- The tool handler parses the arguments with `mcpQuery` and runs the browser route's helper, as before. Results become `*mcp.CallToolResult` with one `TextContent`. A failed audit is a `jsonrpc.Error` with -32603, because a plain error reaches the wire with code 0.
- `mcpTools` becomes `[]*mcp.Tool`. The schemas stay as maps, built by the same helpers. The annotations are read-only, idempotent and closed-world.
- Deleted: the rpc types, `mcpVersion`, `headerValue`, `validID`, `rpcReply`, `mcpLegacy` and `mcpServerInfo`.

### Tests

- `TestMCPToolsMatchTheWebAPI` and `TestMCPReadsWithinTheTokenScope` stay unchanged.
- `TestMCPProtocolEras` is rewritten for what lampi relies on:
  - Initialize era: the versions served, notifications, string ids, the version header, and batching, which the SDK allows only at 2025-03-26.
  - 2026-07-28: `ping` is 404; `clientCapabilities` is required; `tools/list` carries `ttlMs`, `cacheScope` and `serverInfo`; header mismatch, including the base64 `Mcp-Name`; an unsupported version; `server/discover`.
  - Malformed requests are 4xx.
  - Origin, and 405 for GET and DELETE.
- New: the SDK's own client drives the endpoint through `httptest.NewServer`, once per era. This covers the "real client for 2026-07-28" item in TKT-01M445H1.

### Bridge and docs

- `internal/cli` tests run the bridge against the real lake handler, so they run against the SDK unchanged.
- `docs/web-api.md`'s MCP section gets a part on where lampi departs from the SDK's defaults and from the spec, and why. This covers the dependency choices recorded in the ticket description too.
- `docs/architecture.md` and the other docs that call the server hand-written are corrected.
