---
schema: 3
id: TKT-01M3FPWCH4GFYYX4GKT9XFN53G
title: "Read tokens: an events:read permission for agents and MCP clients"
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/auth
  - area/server
  - area/catalog
assignees: []
milestone: null
parent: TKT-01M3FPP3H592T31Y2M3N347CPB
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-26T20:35:36Z
updated_at: 2026-10-01T06:49:12Z
created_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/dae09bda
  name: ""
extensions: {}
---

## Description

### Decision (owner, 2026-10-01)

Agents and MCP clients authenticate with the existing **read tokens** (`lrt_…`,
TKT-01M3NM6FW7), which gain a new permission, `events:read`. A read token is its
own principal:

- an admin mints it
- it is scoped by session UIDs and bays
- it expires within 90 days
- it is revocable and audited as `token:ID (LABEL)`

It does not act as the user who minted it.

This changes decision 4 of the recall epic (TKT-01M3FPP3), which said a
credential "maps to an OIDC identity and its roles". The owner chose a scoped
principal instead. One credential, with one audit trail, covers every agent read
path: the event stream, the `query` CLI and MCP.

### Alternatives rejected

- **A token that acts as the user who minted it.** Each viewer would mint for
  themselves, and each request would read what that user can read at the time.
  It needs a role lookup per request and a self-service minting UI. It also
  makes a token's reach change silently when the user's roles change. An
  explicit bay or session scope is easier to reason about, and it is already
  built and audited.
- **OIDC for MCP clients.** It would need a device or browser flow for every
  agent host, and MCP client support for it varies. This was not chosen for a
  system controlled by one owner.
- **A second token table for agents.** The `permissions` column of
  `read_tokens` was made to grow by permission rather than by table
  (migration comment in `internal/catalog/read_tokens.go`).

### Scope

- Minting chooses `raw:read`, `events:read` or both. Existing tokens keep
  `raw:read` only.
- Each route checks its own permission. A token without the permission gets
  the same answer that a token without `raw:read` gets from the raw route today.
- Device tokens and browser cookies authorize no `events:read` route.
- The permission is the authorization. This ticket adds the permission and the
  check helper. The event stream and MCP tickets add the routes.

## Acceptance criteria

- [x] The chosen mechanism is recorded with the rejected alternative and why.
- [ ] Minting chooses raw:read, events:read or both; existing tokens keep raw:read only; tests cover each.
- [ ] A route requiring events:read refuses a token without it, a device token and a browser cookie, each with a test.
- [ ] docs/web-dashboard.md, docs/web-api.md and decision 4 in docs/web-ui-plan.md state the choice.
