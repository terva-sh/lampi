---
schema: 3
id: TKT-01M3FPWCH4GFYYX4GKT9XFN53G
title: "Read tokens: an events:read permission for agents and MCP clients"
type: task
status: in-progress
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
claim:
  actor: agent:claude-code/dae09bda
  branch: tokens/events-read
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-dae09bda
  commit: 73ecb497243ed15c7307eab04eb9c02654e14046
  session: null
  claimed_at: 2026-10-01T06:58:10Z
  expires_at: null
archive: null
created_at: 2026-09-26T20:35:36Z
updated_at: 2026-10-01T07:11:52Z
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
- [x] Minting chooses raw:read, events:read or both; existing tokens keep raw:read only; tests cover each.
- [x] A route requiring events:read refuses a token without it, a device token and a browser cookie, each with a test.
- [x] docs/web-dashboard.md, docs/web-api.md and decision 4 in docs/web-ui-plan.md state the choice.

## Implementation plan

catalog: PermEventsRead and ReadTokenPermissions beside PermRawRead; CreateReadToken refuses an unknown or repeated permission. No migration: the permissions column already holds a space-separated set, and existing rows keep raw:read. web: the mint form gets a Reads fieldset with one checkbox per permission, none checked to start with, and readTokenRequest refuses a form naming none or an unknown one (checked last, so older forms hear about their other problems first). tokenFor(w, r, perm, realm, now) does the bearer lookup, the 401 challenge and the per-permission 404 that every token route shares; rawByToken uses it, and the event stream and MCP will. Views name what a token reads; the raw curl example shows only for raw:read. Docs: web-dashboard.md, web-api.md, and decision 3 of the recall list in web-ui-plan.md (the epic's decision 4).

## Notes

**agent:claude-code/dae09bda** at 2026-10-01T07:06:00Z

### Evidence
- catalog TestReadTokenPermissions: unknown, repeated and missing permissions are refused; raw:read and events:read are checked one by one.
- web TestReadTokenPermissions: no box checked to start with; an events-only token's panel says normalized events and shows no raw example; an events-only token gets 404 on the raw route; a probe route behind tokenFor(events:read) answers 204 to events and two-permission tokens, 404 to a raw-only token, 401 to a device token, no token, and a browser session (realm lampi-events); mints with no permission or an unknown one are 400; the audit line names permissions=events:read.
- Existing read token tests pass with the permission added to their mint posts.
- `GOFLAGS=-mod=mod just ci` passes.

### Decisions
- A token without the route's permission gets 404, as the ticket said: the same answer as a session outside its scope, so a raw-only token learns nothing about the events routes.
- The read token page still registers only when raw reads are on (reg.Blobs). The event stream ticket (TKT-01M3V3JS) decides whether an events-only lake needs the page without raw.
- No route needs events:read yet. The probe test pins the helper those routes will call.

**agent:claude-code/dae09bda** at 2026-10-01T07:11:52Z

Review 1681 (low): docs/web-ui-plan.md linked this ticket under .tickets/done/ before it moved there. The links now use .tickets/tickets/. Closing this ticket must move both links to .tickets/done/ in the same commit.
