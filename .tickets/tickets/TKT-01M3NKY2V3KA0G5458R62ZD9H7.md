---
schema: 3
id: TKT-01M3NKY2V3KA0G5458R62ZD9H7
title: "Dashboard: admin-only raw artifact view"
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
  - area/auth
  - area/redact
  - policy
assignees: []
milestone: null
parent: TKT-01M3NM61CZDGNG6K2XXGHECC0W
origin: null
dependencies:
  - TKT-01M3NKZT6N7MW8AA0ZZ5ZJWKB9
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-29T03:39:32Z
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

Addendum to `docs/web-ui-plan.md`, which puts raw blobs out of scope
(line 25: "no arbitrary SQL or raw filesystem/CAS access"; line 277:
"Raw blobs and Parquet downloads remain out of scope"). That was written
before the dashboard had roles. The owner decided on 2026-09-28 to let
admins read raw artifacts. This ticket is the dashboard half. Tokens for
tools without a browser session are TKT-01M3NM6FW7 (Raw-read tokens:
admins mint scoped tokens for raw artifacts).

Depends on TKT-01M3NKZT6N (Dashboard: admin role above operator).
Operator is not enough: in the split the Bays epic (TKT-01M3N8KHW5)
decided, operator manages devices and codes, and admin reads everything.

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

### Shape

- A "Raw" tab in the session page's metadata nav (`page.html:19`),
  rendered only for admins. It lists the session's current artifacts,
  each with a download link. Non-admins get no tab, and the routes
  answer 404.
- `GET` of one artifact by session UID and digest. The digest must be an
  artifact of that session, so a caller cannot walk CAS by guessing
  digests. Any other digest is 404, the same answer as a missing one.
- `Content-Disposition: attachment`, `X-Content-Type-Options:
  nosniff`, `Cache-Control: no-store`. Never rendered inline.
- Bounded: 8 MiB by default, with single-range `Range` support for the
  rest. A response cut at the cap says so in a header.
- Reads through `cas.Store.Open`, which already handles `.zst` and
  logical (chunked) objects.
- Every read writes an audit line before any byte is sent: actor,
  session UID, digest, byte range. Never the content. If the line
  cannot be queued, the read is refused.

### Alternatives considered

- **Show the stored `normalize_error` instead.** Worth doing on its own
  (the catalog stores it and the page ignores it), but an error message
  does not show the input that caused it.
- **Gate on operator.** This was the first draft. Rejected because the
  Bays epic separates the power to manage devices from the power to
  read everything, and raw is the most sensitive read there is.
- **CLI only (`serve cat`).** Needs a shell on the lake host, which is
  what the dashboard exists to avoid.
- **Render raw inline as text.** A transcript can carry markup and very
  long lines, and an inline page invites caching and sharing a URL. A
  download keeps the bytes out of the page.

## Acceptance criteria

- [ ] docs/web-ui-plan.md carries a dated addendum that replaces the raw-blob out-of-scope line and links this ticket
- [ ] Raw tab and route render only for admins; operators and viewers get 404 on the route and no tab
- [ ] The route serves only digests that belong to the named session; any other digest is 404
- [ ] Responses are attachments with nosniff and no-store, capped with Range support, and a truncated read is marked
- [ ] Each read writes an audit line (actor, session, digest, range) before any byte is sent; audit failure refuses the read
- [ ] Tests cover non-admin denial, foreign digest, zstd and logical objects, cap and Range, and audit failure
