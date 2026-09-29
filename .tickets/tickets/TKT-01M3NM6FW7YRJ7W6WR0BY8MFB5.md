---
schema: 3
id: TKT-01M3NM6FW7YRJ7W6WR0BY8MFB5
title: "Raw-read tokens: admins mint scoped tokens for raw artifacts"
type: task
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area/auth
  - area/server
  - area/catalog
  - policy
assignees: []
milestone: null
parent: TKT-01M3NM61CZDGNG6K2XXGHECC0W
origin: null
dependencies:
  - TKT-01M3NKY2V3KA0G5458R62ZD9H7
blocks_on: none
references: []
claim:
  actor: agent:claude-code/cd41c9ac
  branch: web/read-tokens
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-fdd1a9d1
  commit: e7ed2db264e2c020a41428f612753bde686917e8
  session: null
  claimed_at: 2026-09-29T03:59:47Z
  expires_at: null
archive: null
created_at: 2026-09-29T03:44:08Z
updated_at: 2026-09-29T04:04:58Z
created_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
extensions: {}
---

## Description

Let an admin mint a token that fetches raw session artifacts, for
scripts and tools that have no browser session. Split out of
TKT-01M3NKY2V3 (Dashboard: admin-only raw artifact view) to keep each PR
small enough for terva-review. It serves the same artifact reads that
ticket builds, so it depends on it.

### Shape

- An admin mints a token on an admin page under `/admin/`. Minting
  adds access to the lake, so it requires a fresh sign-in, as minting a
  registration code does (`webauth.FreshWindow`).
- The token is shown once. The catalog stores its SHA-256 only, in a
  new table, the way `registrations.secret_sha256` and
  `devices.token_sha256` are stored. The token is 32 random bytes with
  a recognisable prefix, so that a leaked one is easy to find in logs
  and secret scanners.
- Each token has a label, a scope, and an expiry. The scope is the whole
  lake or a list of session UIDs. The expiry has a default and a
  maximum, so no token is permanent.
- Admins list tokens (label, scope, created, created by, expires, last
  used, state) and revoke one. Revocation takes effect on the next
  request.
- The token authenticates with `Authorization: Bearer` on a raw
  artifact route and on nothing else: no dashboard page, no search, no
  export, no `/v1` ingestion. The route reuses the raw view's handler,
  so it gets the same digest check, cap, Range, and headers.
- A device upload token never reads, and a raw-read token never
  uploads.
- Minting, revoking and every read are audited before they take effect.
  The actor of a read is the token's ID and label.

### Relation to TKT-01M3KAMD1Z

TKT-01M3KAMD1Z (Lake-backed sessions: read path, token scope, byte-exact
reads) plans "a plain bearer token with a dedicated global search/read
permission" and says "the device upload token is not the read
credential". It is on the unpushed branch `t3code/session-lake-epic`
and has no implementation. Build the narrow version here, as a token
kind with one permission (`raw:read`) stored in a permissions column
rather than implied by the table, so that TKT-01M3KAMD1Z can add
permissions to it instead of building a second system. Record in this
ticket what it should know when it starts.

## Acceptance criteria

- [x] Admins can mint, list and revoke raw-read tokens; minting needs a fresh sign-in; operators and viewers get 404 on these routes
- [x] A token is shown once, stored as a SHA-256 only, carries a label, a scope (lake or named sessions), an expiry with a default and a maximum, and a permissions set holding raw:read
- [x] A token reads only the raw artifact route within its scope; out-of-scope, expired, revoked and unknown tokens get the same 404 or 401 answers as the route gives any unauthorized caller
- [x] A device token cannot read raw, and a raw-read token is refused on /v1
- [x] Mint, revoke and every read write an audit line before they take effect; audit failure refuses the request
- [x] docs/web-api.md documents the token route and docs/web-dashboard.md the admin page
- [x] Tests cover scope, expiry, revocation, the device-token and /v1 separation, and audit failure
- [x] The ticket records what TKT-01M3KAMD1Z should reuse from this token model

## Implementation plan

Catalog: read_tokens table (migration 17), ReadToken with State/Allows, Create/Revoke queue read_token.created/revoked in the same transaction, TouchReadToken for last use. Web: /admin/read-tokens page (AdminOnly; mint needs a fresh sign-in and uses mintAttempts against double submits; token shown once as lrt_ + 32 random bytes), revoke form, and GET /api/raw/v1/sessions/{uid}/artifacts/{sha256} outside Guard, which checks the bearer, scope and expiry, then calls serveRaw with actor token:ID (LABEL). Docs: web-api, web-dashboard, web-ui-plan addendum.

## Notes

**agent:claude-code/cd41c9ac** at 2026-09-29T04:04:57Z

### For TKT-01M3KAMD1Z (Lake-backed sessions: read path, token scope, byte-exact reads)

That ticket was on the unpushed branch `t3code/session-lake-epic` when
this landed, so the note lives here. When it starts, reuse these tokens
rather than building a second kind:

- **Table.** `read_tokens` (catalog migration 17, `migrateReadTokens`):
  `token_sha256` unique, `permissions` a space-separated set, `sessions` a
  JSON array or `''` for the whole lake, and `expires_at` always set.
- **Check.** `catalog.ReadToken.Allows(perm, sessionUID, now)` is the one
  check. A new read, such as search, adds a permission constant beside
  `PermRawRead` and calls `Allows` with it.
- **Byte-exact reads.** `web.serveRaw` already serves byte-exact raw
  artifacts through `cas.Store.Open`, which reads compressed, logical and
  prefix-record objects.
- **Route and auth.** The token route is under `/api/raw/v1`, outside the
  browser `Guard` and outside `/v1`. Device tokens stay upload-only.
  `readTokenOf` in `internal/web/read_tokens.go` parses the bearer, and
  can move to a shared package if MCP needs it.
- **What it does not have.** Minting is dashboard-only, with no CLI and
  no API. Scope is the lake or a session list: no project or bay scope.
  A device cannot ask for a token that a person approves.

### Alternatives considered

- **Reuse the device token table with a role column.** Rejected: a
  stolen device token must never read, and one table would make that a
  column check instead of a separate credential.
- **Put the route under /v1.** Rejected: /v1 is the ingestion protocol,
  authenticated by device tokens. Keeping reads out of it keeps its
  contract unchanged.
- **Accept the browser session on the token route too.** Rejected: the
  route is for tools, and a cookie there would open it to CSRF-style
  cross-site GETs. Browsers use `/sessions/{uid}/raw/{sha256}`.
