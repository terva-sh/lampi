---
schema: 3
id: TKT-01M3NKY2V3KA0G5458R62ZD9H7
title: "Raw artifact access for admins: dashboard view and read tokens"
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
  - area/auth
  - area/redact
  - policy
  - area/protocol
assignees: []
milestone: null
parent: null
origin: null
dependencies:
  - TKT-01M3NKZT6N7MW8AA0ZZ5ZJWKB9
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-29T03:39:32Z
updated_at: 2026-09-29T03:40:57Z
created_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
extensions: {}
---

## Description

Addendum to `docs/web-ui-plan.md`, which puts raw blobs out of scope
(line 25: "no arbitrary SQL or raw filesystem/CAS access"; line 277:
"Raw blobs and Parquet downloads remain out of scope"). That was written
before the dashboard had a permission system. The owner decided on
2026-09-28 to allow reading raw artifacts through two paths, both
restricted to admins:

1. An admin reads a raw artifact in the dashboard.
2. An admin mints a token that can fetch raw transcripts, for scripts
   and tools that have no browser session.

Depends on TKT-01M3NKZT6N (Dashboard: admin role above operator).
Operator is not enough: in the role split the Bays epic decided
(TKT-01M3N8KHW5), operator mints registration codes and need not see
transcripts, while admin reads everything.

### Why

When normalization fails, the session page shows no transcript and says
"operator logs hold the details". Reading the raw artifact is the only
way to see what the projector choked on without a shell on the lake
host. Seen on a terva session in `zot` whose head is one 672 KB
`transcript_jsonl`.

### Why raw needs the admin gate

Raw bytes are the least filtered copy in the lake. Ruleset v2
quarantines a hit but never rewrites the file (`docs/policy.md`), so a
file allowed through with `quarantine allow` or `redaction.upload_hits`
still holds the secret. Normalized views and exports strip or disclose;
raw does neither.

### Path 1: dashboard

- A "Raw" tab in the session page's metadata nav (`page.html:19`),
  rendered only for admins. It lists the current artifacts, each with a
  raw link. Non-admins get no tab, and the route answers 404.
- `GET` of one artifact by session UID and digest. The digest must be an
  artifact of that session, so a caller cannot walk CAS by guessing
  digests.
- `Content-Disposition: attachment`, `X-Content-Type-Options: nosniff`,
  `Cache-Control: no-store`. Never rendered inline as HTML.
- Bounded: a byte cap, 8 MiB by default, with `Range` support for the
  rest. A truncated response says so in a header and never reports a
  complete read.
- Decompresses `.zst` objects the same way `internal/cas` reads them.

### Path 2: raw-read tokens

- An admin mints a token on an admin page. It is shown once, then only
  its hash is stored, as with device tokens and registration codes.
- A raw-read token is a separate kind of credential. A device upload
  token never reads, and a raw-read token never uploads. This matches
  TKT-01M3KAMD1Z (Lake-backed sessions: read path, token scope,
  byte-exact reads): "The device upload token is not the read
  credential."
- Scope at minting: the whole lake, or a list of session UIDs. Every
  token has an expiry, with a default and a maximum, and a label saying
  what it is for.
- Admins can list tokens (label, scope, created, expires, last used) and
  revoke one. Revocation takes effect on the next request.
- The token authenticates the same artifact route as Path 1, with the
  same digest check, cap, Range, and headers. It grants nothing else:
  no dashboard pages, no search, no export.
- Coordinate with TKT-01M3KAMD1Z before building. If that ticket's token
  scope model has landed, a raw-read token is one permission in it, not
  a second token system. If it has not, build the narrow version here
  and note in TKT-01M3KAMD1Z that it exists.

### Audit (both paths)

Every read writes an audit line before serving: the actor (admin
identity, or the token's ID and label), session UID, digest, byte range,
and result. Never the content. Minting and revoking a token are audited
too. A read whose audit line cannot be written is refused.

### Alternatives considered

- **Show the stored `normalize_error` instead.** Worth doing on its own
  (the catalog stores it and the page ignores it), but an error message
  does not show the input that caused it.
- **Gate on operator.** This was the first draft. Rejected because the
  Bays epic separates the power to manage devices from the power to
  read everything, and raw is the most sensitive read there is.
- **CLI only (`serve cat`).** Needs a shell on the lake host, which is
  what the dashboard and tokens exist to avoid.
- **Let device tokens read.** Rejected: a stolen device token would read
  every transcript in the lake, not just upload into it.

## Acceptance criteria

- [ ] docs/web-ui-plan.md carries a dated addendum that replaces the raw-blob out-of-scope line and links this ticket
- [ ] Raw tab and route render only for admins; operators and viewers get 404 on the route and no tab
- [ ] The route serves only digests that belong to the named session; any other digest is 404
- [ ] Responses are attachments with nosniff and no-store, capped with Range support, and a truncated read is marked
- [ ] Admins can mint, list and revoke raw-read tokens; a token is shown once, stored hashed, has an expiry, and is scoped to the lake or to named sessions
- [ ] A raw-read token reads only the artifact route within its scope; it cannot upload, and a device token cannot read
- [ ] Every read, mint and revoke writes an audit line before it takes effect; audit failure refuses the request
- [ ] Token design is reconciled with TKT-01M3KAMD1Z, and the outcome is noted in both tickets
- [ ] Tests cover non-admin denial, foreign digest, zstd object, cap and Range, token scope, expiry, revocation, and audit failure
