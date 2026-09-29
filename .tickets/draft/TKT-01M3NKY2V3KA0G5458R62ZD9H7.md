---
schema: 3
id: TKT-01M3NKY2V3KA0G5458R62ZD9H7
title: "Dashboard: admin-only raw artifact view (addendum to web UI scope)"
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
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-29T03:39:32Z
updated_at: 2026-09-29T03:39:32Z
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
"Raw blobs and Parquet downloads remain out of scope"). That line was
written before the dashboard had a permission system. It now has one:
`role_map` grants `viewer` or `operator`, and operator-only routes go
through `webauth.OperatorOnly` (`internal/web/device_actions.go:24`,
`profile_edit.go:42`). The owner decided on 2026-09-28 to allow raw
access through the dashboard for admins only.

### Why

When normalization fails, the session page shows no transcript and says
"operator logs hold the details". Reading a raw artifact is the only way
to see what the projector choked on without a shell on the lake host.
Seen on a terva session in `zot` whose head is one 672 KB
`transcript_jsonl`.

### What "admin" means here

The `operator` role. No separate `admin` role is added. Viewers never
see the link or the route, which answers 404 for them, not 403, so a
viewer cannot even learn that raw access exists.

### Why raw needs the admin gate

Raw bytes are the least filtered copy in the lake. Ruleset v2
quarantines a hit but never rewrites the file (`docs/policy.md`), so a
file allowed through with `quarantine allow` or `redaction.upload_hits`
still holds the secret. Normalized views and exports strip or disclose;
raw does neither.

### Shape

- A "Raw" tab in the session page's metadata nav (`page.html:19`),
  rendered only for operators. It lists the current artifacts, each with
  a raw link.
- `GET` of one artifact by session UID and digest. The digest must be an
  artifact of that session, so a caller cannot walk CAS by guessing
  digests.
- Response `text/plain; charset=utf-8` or `application/octet-stream`
  with `Content-Disposition: attachment`, `X-Content-Type-Options:
  nosniff`, and `Cache-Control: no-store`. Never rendered inline as HTML.
- Bounded: a byte cap, 8 MiB by default, with `Range` support for the
  rest. A truncated response says so in a header and never reports a
  complete read.
- Audited like device actions: actor, session UID, digest, byte range,
  result. Never the content. A request whose audit line cannot be
  written is refused, not served.
- Decompresses `.zst` objects the same way `internal/cas` reads them.

### Alternatives considered

- **Keep raw out of scope and show the stored `normalize_error`
  instead.** Worth doing on its own (the catalog already stores it and
  the page ignores it), but an error message does not show the input
  that caused it.
- **Add a new `exporter` or `admin` role.** More config and migration
  for one route. The operator already mints registration codes and edits
  the profiles that decide what gets collected, so it is already trusted
  above a viewer.
- **CLI only (`serve cat`).** Needs a shell on the lake host, which is
  the thing the dashboard exists to avoid.

## Acceptance criteria

- [ ] docs/web-ui-plan.md carries a dated addendum that replaces the raw-blob out-of-scope line and links this ticket
- [ ] Raw tab and route render only for operators; a viewer gets 404 on the route and no tab
- [ ] The route serves only digests that belong to the named session; any other digest is 404
- [ ] Responses are attachments with nosniff and no-store, capped with Range support, and a truncated read is marked
- [ ] Each request writes an audit line (actor, session, digest, range, result) before serving; audit failure refuses the request
- [ ] Tests cover viewer denial, foreign digest, zstd object, cap and Range, and audit failure
