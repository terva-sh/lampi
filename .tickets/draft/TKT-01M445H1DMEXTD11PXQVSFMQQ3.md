---
schema: 3
id: TKT-01M445H1DMEXTD11PXQVSFMQQ3
title: "MCP follow-ups: bay-token tests, bridge limits, audit rate"
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
  - area/search
assignees: []
milestone: null
parent: TKT-01M3FPP3H592T31Y2M3N347CPB
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-04T19:16:21Z
updated_at: 2026-10-04T19:16:21Z
created_by:
  id: agent:claude-code/9078ac3f
  name: ""
updated_by:
  id: agent:claude-code/9078ac3f
  name: ""
extensions: {}
---

## Description

Follow-ups from TKT-01M3FPWCF (MCP: serve recall tools over the shared query layer), which shipped in #186 and #187. Each item came from terva-review or from the second-model review of the run's decision trail. None blocked the merge, and none is a known leak today.

### Tests

- An MCP-level test for a bay-scoped read token, and for one scoped to both bays and sessions. Today only a session-named token is exercised through `/api/read/v1/mcp`. Bay scoping is tested at the catalog level only, where `TestBayScopedReadTokens` checks that `ReadTokenScope` agrees with `ReadTokenReaches`.
- A real-client check of the 2026-07-28 era, once an MCP SDK ships it. The TypeScript SDK 1.32.0 negotiates 2025-11-25, so the per-request `_meta` path is covered only by tests written from the spec.

### The stdio bridge, `internal/cli/mcp.go`

- Log the lake's refusal of a notification to stderr (review 2115, low). Today `forward` returns before it checks the answer to a notification, so a revoked token shows nothing until the next request.
- Bound concurrency. `serve` starts one goroutine and one HTTP request per message, with no limit.
- Honour `notifications/cancelled` by cancelling that request's HTTP call.

### The lake endpoint, `internal/web/mcp.go`

- Every `tools/call`, refused or not, queues and flushes an audit line. Consider a rate limit per token. The MCP spec says a server must rate-limit tool calls.
- `catalog.SessionBayNames` reads `scope.all` and `scope.bays` directly and ignores a session narrowing. Its callers check the session first, so this is latent. Make it go through the same check as `where`, so a later caller cannot miss it.

### Recorded for whoever revisits the SDK question

The run wrote the MCP server by hand rather than adding `modelcontextprotocol/go-sdk`, on the grounds that `go.mod` has eight direct dependencies. It did not measure the SDK's own dependency tree. Six of the eight review findings were protocol corners in the hand-written code: notification and `initialize` header checks, id types, and argument handling. Measure the SDK before the next protocol revision rather than after.
