---
schema: 3
id: TKT-01M445H1DMEXTD11PXQVSFMQQ3
title: "MCP follow-ups: bay-token tests, bridge limits, audit rate"
type: task
status: in-progress
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
references:
  - ref: pr:199
    path: null
  - ref: pr:200
    path: null
  - ref: pr:201
    path: null
claim:
  actor: agent:claude-code/9078ac3f
  branch: mcp/bay-scoped-tokens
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-268e1246
  commit: d807aaa779b3399b6fa1e8d44e4593bd5f208f42
  session: null
  claimed_at: 2026-10-04T22:37:13Z
  expires_at: null
archive: null
created_at: 2026-10-04T19:16:21Z
updated_at: 2026-10-04T22:51:38Z
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

## Acceptance criteria

- [x] An MCP test reads through a bay-scoped read token, and through one scoped to a bay and named sessions, and each reaches only its sessions.
- [x] SessionBayNames answers no bays for a session outside a scope narrowed to named sessions.
- [x] The lake limits tool calls per read token; a call over the limit is refused with a hint and no audit line, and docs/web-api.md gives the limit.
- [x] terva-lampi mcp logs to stderr the lake's refusal of a notification.
- [x] terva-lampi mcp keeps a bounded number of requests to the lake in flight.
- [x] terva-lampi mcp cancels a request's call to the lake on notifications/cancelled, and writes no answer for it.
- [x] A real client drives the 2026-07-28 era.

## Implementation plan

The work comes in three PRs, each small enough for terva-review. They feed the next release, v0.8.0, which the owner asked to deploy to brokkr's lake and agent on 2026-10-04.

### PR 1: bay-scoped tokens through the endpoint, and SessionBayNames

- `catalog.SessionBayNames` returns no bays for a session that a scope narrowed to named sessions does not name. This is the check `where` makes. A new `Scope` helper answers "does this scope name this uid" so that later callers share it. Catalog test.
- `internal/web/mcp_test.go` gets two MCP-level tests. A token scoped to one bay reaches that bay's sessions only, through search, `read_events` and `copy_excerpt`. A token scoped to a bay and to named sessions reaches only a named session that is also in the bay. Neither sees `coverage`.

### PR 2: a per-token limit on tool calls

The tools spec says a server must rate-limit tool invocations, and each call queues and flushes a synced audit line.

- A `golang.org/x/time/rate` limiter per read token sits in the audit middleware, ahead of the audit.
  - It refills 2 calls a second, with a burst of 30. An agent's bursts of parallel searches pass, and a runaway loop is held to 2 synced writes a second.
  - It is keyed by token id. The map grows only with minted tokens, which an admin creates.
- A refused call is a tool result with `isError` set, the body `{"error":"rate_limited"}` and a hint naming the wait. The agent sees it as it sees any other tool error and can back off.
- It writes no audit line. The limiter is what bounds the synced writes, as for the open routes.
- The limiter takes the gate's `now`, which carries Go's monotonic reading in production. A wall-clock step does not refill it, which is the concern docs/architecture.md raises for the open-route limiter.

### PR 3: the stdio bridge

- **Log a notification the lake refused** (review 2115). `forward` now reads the status of a notification's answer and logs a refusal to stderr.
- **Bound concurrency.** At most 8 requests are in flight to the lake. When all 8 slots are taken, the reader waits for one, which holds back reading stdin. initialize still runs on its own first.
- **notifications/cancelled.**
  - The bridge keeps a cancel function per in-flight request id. On `notifications/cancelled` it cancels that request's HTTP call and writes no answer for it, as the cancellation spec asks of a receiver.
  - The notification itself is not forwarded. Over HTTP, closing the request is the cancellation, and the stateless lake has nothing to cancel.

The criterion for a real client of 2026-07-28 is met by TestMCPServesTheSDKClient (TKT-01M44DVPVX, PR 195).

## Notes

**agent:claude-code/9078ac3f** at 2026-10-04T21:59:56Z

Two items here are answered by TKT-01M44DVPVXQVWVGC3EYAFH64RP (MCP: serve the lake endpoint with the official Go SDK), PR 195. The SDK question: the endpoint now runs on modelcontextprotocol/go-sdk v1.8.0, with the measurements and alternatives in that ticket. The real-client check of 2026-07-28: TestMCPServesTheSDKClient drives the endpoint with the SDK's own client at 2026-07-28, 2025-11-25 and 2025-03-26. The bridge items, the per-token rate limit (x/time/rate now ships with the SDK) and SessionBayNames are still open.

**agent:claude-code/9078ac3f** at 2026-10-04T22:51:38Z

### The bridge, PR 201

PRs: #199 (bay-scoped tokens, SessionBayNames), #200 (rate limit), #201 (bridge). They are stacked in that order.

Criteria 4–6 are met by `TestMCPBridgeLogsARefusedNotification`, `TestMCPBridgeBoundsRequestsInFlight` and `TestMCPBridgeCancelsARequest`. Each was run against the bridge before this change and failed for the reason it names:

- No 404 line on stderr.
- 24 requests open at once, against a cap of 8.
- The lake call was never cancelled; the test waited 10s.

### Alternatives considered for the bound

- **A second, larger bound on lines read but not yet sent**, so a `notifications/cancelled` is read even while all 8 slots are taken. Rejected. It moves the same corner to the larger bound rather than removing it, and it adds a second limit to explain. With one bound, a cancellation waits only when more than 8 requests are outstanding. Even then it waits only until the lake answers one of them.
- **A goroutine per message that waits for a slot.** Rejected: the number of goroutines and buffered lines is unbounded again, which is what the bound is for.

### Alternatives considered for the cancellation

- **Forwarding `notifications/cancelled` to the lake.** Rejected. The lake is stateless, so its server has no request of that id to stop, and forwarding would cost a request. Closing the HTTP request already ends the lake's handler context.
- **Writing a JSON-RPC error for the cancelled id.** Rejected. The cancellation spec asks a receiver not to answer a cancelled request, and the agent has already stopped waiting for it.
