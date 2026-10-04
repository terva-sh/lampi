---
schema: 3
id: TKT-01M3FPWCFSFK9572R9MCFPCK60
title: "MCP: serve recall tools over the shared query layer"
type: task
status: done
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
dependencies:
  - TKT-01M3F2PGMZKTFXSX521T07A4HA
  - TKT-01M3FPWCH4GFYYX4GKT9XFN53G
  - TKT-01M3FPWC9E15XG1GFS7886Z415
  - TKT-01M3FPWCBK7WQSRF723RJFXKXE
  - TKT-01M3FPWCDFXXCHD8F5PA0GGMWP
blocks_on: none
references:
  - ref: pr:186
    path: null
  - ref: pr:187
    path: null
claim: null
archive: null
created_at: 2026-09-26T20:35:36Z
updated_at: 2026-10-04T19:17:07Z
created_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/9078ac3f
  name: ""
extensions: {}
---

## Description

Serve an MCP endpoint from the lake exposing search, structured filters, event windows around a hit, deep links and copy-out as tools. Tools call the shared recall layer and return its result shapes unchanged. This replaces terva-ext-session-search for agents on the owner's machines. Blocked on the MCP auth decision (sibling ticket); prompt injection handling is out of scope per the epic.

## Acceptance criteria

- [x] MCP tools return the same results as the web API for the same inputs.
- [x] Documentation shows how to configure an agent to use the lake for recall instead of terva-ext-session-search.

## Implementation plan

Two pull requests, so each stays small enough for terva-review.

### PR 1: the lake serves MCP (this branch)

- `catalog.ReadTokenScope` turns a read token into a `catalog.Scope`: every bay for a token from before bays, otherwise the bays it holds read on now, narrowed by the new `Scope.OnlySessions` to the sessions it names. Every recall function already enforces access through a Scope, so the token reaches search, event pages and excerpts without filtering each hit by hand. A narrowed scope reports `All()` false, so search coverage stays hidden, and `Reads(bays)` answers false because bays alone cannot say whether a named session is in it.
- `POST /api/read/v1/mcp` in `internal/web/mcp.go`, authenticated by `tokenFor` with `events:read`. Three tools, `search`, `read_events` and `copy_excerpt`, call the same server helpers as the browser routes (`runSearch`, `eventPage`, `excerpt`, extracted from the handlers in `server.go`). Tool arguments become the route's query parameters and go through the route's own parser, so the same input gives the same result by construction. Links are made absolute with `base_url`.
- Tool errors carry the browser route's error body (`failure`, extracted from `fail`) plus a hint for the agent.
- Each `tools/call` is audited as `events.read` with an `mcp tool=` detail. Search text is recorded by its length only.
- Both protocol eras on one stateless route: initialize-era clients (2025-03-26 to 2025-11-25) and per-request `_meta` clients (2026-07-28), including header and body matching and `server/discover`.
- Reference docs in `docs/web-api.md`.

### PR 2: `terva-lampi mcp` and agent setup

- A stdio MCP server in the CLI that reads the token from `--token-file` and forwards each message to the lake's endpoint, so an agent's MCP config names a file, not the secret.
- `docs/reading-the-lake.md`: configure Claude Code and Codex to use the lake for recall instead of `terva-ext-session-search`.

### Alternatives rejected

- **The official Go MCP SDK.** `go.mod` has eight direct dependencies, and the server needs only `initialize`, `server/discover`, `ping`, `tools/list` and `tools/call` with JSON replies. The SDK's own dependency tree was not measured. In hindsight, six of the eight review findings were protocol corners the SDK would likely have handled; TKT-01M445H1 asks to measure it before the next protocol revision.
- **Filtering hits by `t.Allows` in the adapter.** Search pages would hold fewer hits than the limit for no visible reason, and each new tool would have to remember the filter. The Scope makes the catalog refuse what the token cannot read.
- **Making links absolute inside `recall`.** It would add an input to `SearchRequest` and `EventRequest` that only one adapter sets, and would change the search cursor fingerprint. The excerpt already takes `Origin` because its text is pasted elsewhere; a link in JSON is the adapter's presentation.
- **`structuredContent` and `outputSchema`.** The spec asks for the same JSON in a text block as well, which would double what reaches the agent's context.
- **Only the 2026-07-28 era.** It is two months old, and a client that still sends `initialize` would fail with no way forward.

## Notes

**agent:claude-code/dae09bda** at 2026-10-01T06:49:12Z

MCP authenticates with read tokens holding events:read, per TKT-01M3FPWCH4GFYYX4GKT9XFN53G. Tools should reuse the internal/recall event filter from TKT-01M3V3J8VZKAZJJAR9VTMDGJGD.

**agent:claude-code/9078ac3f** at 2026-10-04T18:44:20Z

The lake endpoint is up for review as Forgejo PR 186, and the stdio bridge with the agent docs as PR 187, which builds on 186.

### Protocol era

The MCP spec revision 2026-07-28 drops the `initialize` handshake and puts the version, client info and capabilities in every request's `_meta`, mirrored into the `MCP-Protocol-Version`, `Mcp-Method` and `Mcp-Name` headers. The current TypeScript SDK (1.32.0) still speaks 2025-11-25 with `initialize`. So the endpoint serves both eras on one stateless route, as the spec allows a dual-era server to do. Supporting only 2026-07-28 would have left today's agents with no way in, and supporting only the initialize era would fail a modern client's header checks.

### Review 2102 on PR 186

terva-review found two audit gaps. A misnamed argument's value was written to the audit log before the parser refused it, and refused calls returned before the audit step. Fixed in b3a5f60: every call is audited first, and the detail takes only the keys in the tool's input schema. The new assertions fail on the earlier commit.

### Verification beyond unit tests

The official TypeScript MCP SDK client ran against a throwaway localhost lake: over HTTP for PR 186, and through the built `terva-lampi mcp` over stdio for PR 187. Both runs listed the tools and called each one.

### Unrelated findings, filed as drafts

- TKT-01M44347FD (Flaky under load: sqlitesnap TestTakeIsConsistentUnderAWriter)
- TKT-01M44347GZ (go.mod lists zeebo/blake3 as indirect; -mod=mod rewrites it)

**agent:claude-code/9078ac3f** at 2026-10-04T19:16:34Z

### Review rounds after the first note

terva-review ran six times on the two PRs and raised eight findings, each fixed with a test that fails on the commit before the fix:

- 2102: two audit gaps, covered in the earlier note.
- 2103: search dropped a `session_uid` argument instead of refusing it, so an agent narrowing a search to one session got every session's hits.
- 2105: notifications skipped the version and header checks, and object, array and boolean ids were accepted.
- 2107, then 2111: a failed stdout write in the bridge was ignored. The first fix covered only the synchronous `initialize` path. The second reads stdin on its own goroutine, so a failed write ends `serve` while stdin is idle.
- 2112: `initialize` skipped the header check.
- 2115, low, not fixed: the bridge does not log the lake's refusal of a notification. Filed in TKT-01M445H1.

The last reviews on both heads were clean at the failure threshold.

### Second-model review of the decision trail

An investigator subagent on a different model (Sonnet) read the decision log, the session digest and the diff. Acted on: rerunning the stdio SDK client against the merged binary (it passed), removing the scratch worktree, closing this ticket, and correcting the unmeasured SDK claim in the plan. Filed as TKT-01M445H1: an MCP-level bay-scoped token test, a real-client check of the 2026-07-28 era, bridge concurrency and cancellation, an audit rate limit, and `SessionBayNames` ignoring a session narrowing. Reported to the owner, not changed: `git ticket claim` writes the local worktree path into the ticket, and the GitHub sync published it, as it already had for eight other tickets.

## Summary

Shipped in #186 and #187, merged to main at 3acce92 and synced to GitHub.

- The lake serves MCP at `POST /api/read/v1/mcp` to read tokens holding `events:read`, with three tools: `search`, `read_events` and `copy_excerpt`. Each calls the same helper as its browser route, and the route's own parser reads the tool's arguments. `TestMCPToolsMatchTheWebAPI` checks the results match, with links made absolute.
- `catalog.ReadTokenScope` turns a token into a `Scope`, narrowed by `Scope.OnlySessions` to any sessions it names, so the catalog refuses what the token cannot read.
- The endpoint is stateless. It serves initialize-era clients (2025-03-26 to 2025-11-25) and 2026-07-28 per-request `_meta` clients. Every `tools/call` is audited as `events.read` before it runs, refused calls included, with only the arguments the tool names, and with search text recorded by length.
- `terva-lampi mcp` is a stdio bridge. An agent's MCP configuration names the token file, never the token. `docs/reading-the-lake.md` shows how to add it to Claude Code and Codex in place of `terva-ext-session-search`.
- The official TypeScript MCP SDK client (1.32.0) drove both the endpoint and the bridge built from merged main.
- Follow-ups are in TKT-01M445H1 (MCP follow-ups: bay-token tests, bridge limits, audit rate). Two unrelated drafts came out of the run: TKT-01M44347FD (sqlitesnap load flake) and TKT-01M44347GZ (`go.mod` blake3 drift).
- The decision trail was distilled into the plan and notes here and is not committed, because its evidence cells name the internal forge.
