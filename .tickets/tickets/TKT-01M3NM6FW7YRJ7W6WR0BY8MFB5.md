---
schema: 3
id: TKT-01M3NM6FW7YRJ7W6WR0BY8MFB5
title: "Raw-read tokens: admins mint scoped tokens for raw artifacts"
type: task
status: ready
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
claim: null
archive: null
created_at: 2026-09-29T03:44:08Z
updated_at: 2026-09-29T03:44:52Z
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

- [ ] Admins can mint, list and revoke raw-read tokens; minting needs a fresh sign-in; operators and viewers get 404 on these routes
- [ ] A token is shown once, stored as a SHA-256 only, carries a label, a scope (lake or named sessions), an expiry with a default and a maximum, and a permissions set holding raw:read
- [ ] A token reads only the raw artifact route within its scope; out-of-scope, expired, revoked and unknown tokens get the same 404 or 401 answers as the route gives any unauthorized caller
- [ ] A device token cannot read raw, and a raw-read token is refused on /v1
- [ ] Mint, revoke and every read write an audit line before they take effect; audit failure refuses the request
- [ ] docs/web-api.md documents the token route and docs/web-dashboard.md the admin page
- [ ] Tests cover scope, expiry, revocation, the device-token and /v1 separation, and audit failure
- [ ] The ticket records what TKT-01M3KAMD1Z should reuse from this token model
