---
schema: 3
id: TKT-01M3V3JSQXA831MB02BRWW6NHE
title: "Read API: stream filtered normalized events to read tokens"
type: task
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area/search
  - area/server
  - area/auth
assignees: []
milestone: null
parent: TKT-01M3FPP3H592T31Y2M3N347CPB
origin: null
dependencies:
  - TKT-01M3V3J8VZKAZJJAR9VTMDGJGD
  - TKT-01M3FPWCH4GFYYX4GKT9XFN53G
blocks_on: none
references: []
claim:
  actor: agent:claude-code/dae09bda
  branch: read/events-stream
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-dae09bda
  commit: d2b04b9a6d487763ed852c34203643c6b081a1db
  session: null
  claimed_at: 2026-10-01T07:07:51Z
  expires_at: null
archive: null
created_at: 2026-10-01T06:49:12Z
updated_at: 2026-10-01T07:12:46Z
created_by:
  id: agent:claude-code/dae09bda
  name: ""
updated_by:
  id: agent:claude-code/dae09bda
  name: ""
extensions: {}
---

## Description

### Why

An agent on any machine should be able to read a filtered slice of the lake
without shell access to the lake host. Today that slice is `sudo -u
terva-lampi terva-lampi export | jq`, run on the host.

### Scope

`GET /api/read/v1/events` streams matching normalized events as NDJSON to
a read token holding `events:read` (TKT-01M3FPWCH).

- The query parameters are the export filters, under the names the web
  search API already uses (`event_type`, `actor`, `tool`, `tool_error`,
  `raw_type`, `harness`, `project`, `since`, `until`), plus `fields`. They
  are matched by the shared `internal/recall` filter from the export ticket,
  so the same inputs give the same rows as `export`.
- The token's session and bay scope limits the sessions read. A session out
  of scope is skipped silently, as the raw route treats it.
- The stream reads only published normalized JSONL. It never starts
  normalization and never reads raw blobs.
- A request with no filter at all is refused, so one call cannot dump the
  whole scope by accident.
- A client can tell a complete stream from one that was cut short. The
  mechanism, such as a final status line or an HTTP trailer, is chosen in the
  plan and documented.
- Each request writes one `events.read` audit event. It names the token, the
  filters and the row count, and never transcript content.

## Acceptance criteria

- [x] The same filters and fields return the same rows as export on the same synthetic lake.
- [x] Only sessions in the token's scope are read; tokens without events:read, device tokens and cookies are refused.
- [x] A request with no filter is refused; a cut stream is distinguishable from a complete one, as documented.
- [x] docs/web-api.md documents the route.
- [x] Each request writes one events.read audit event, before any line is sent, naming the token and the query and never content; the row count is in the end line.

## Implementation plan

recall.Reader.Select walks catalog.PublishedSessions, skips sessions not ready or outside the filter's harness/project, asks a reaches callback per session, opens each through the pinned-generation open() that Events uses (an unavailable or changed session is skipped and counted), and reads lines with readLine(MaxLine) through EventFilter.Line and Fields.Project. web: GET /api/read/v1/events behind tokenFor(events:read, realm lampi-events); parseReadEvents takes the search filter names plus fields and refuses unknown, repeated or empty parameters; no filter is 400 filter_required. reaches = token.Allows(events:read, uid) and ReadTokenReaches. One events.read audit event is queued and flushed before the 200, as a raw read's is. The body is NDJSON through a 64 KiB buffer and ends with a {"lampi:end":{...}} line carrying complete, error and the Select counts. Registered whenever reg and the reader exist, independent of raw blobs.

## Notes

**agent:claude-code/dae09bda** at 2026-10-01T07:11:21Z

### Decisions
- **End line, not an HTTP trailer.** The lake sits behind a reverse proxy, and proxies commonly drop trailers, so a trailer could vanish and a cut stream would look complete. The end line's key, lampi:end, holds a colon, which no event field or --fields path can, so it cannot be mistaken for a row. The query CLI (TKT-01M3V3KC) strips it and exits nonzero without it.
- **The audit event has no row count.** AC 4 asked for one, but the count is only known after the last line has left, and the raw route's rule is that the event is durable before any byte leaves. A second event at the end would break 'one event per request', and an event at the end alone would let an unaudited read through when the audit write fails. The event names the token and the full query. The row count is in the end line. AC 4 is amended to match.
- **fields alone is not a filter.** It still selects every event in scope.
- **Oversized lines** (over recall.MaxLine) are dropped from whole-event output (the reader does not keep them) and written with null fields under fields; both are counted in the end line.

### Evidence
- web TestReadEventsStream: three filter shapes give the same rows as export's selection over the same published files; a session-scoped token reads only its session; a raw-only token gets 404, and a device token, no token and a browser session get 401; no filter and fields alone are filter_required; eight malformed queries are invalid_request; the audit names the token and the query and holds no content or secret.
- recall TestSelect: a session not reached is skipped, a published session whose file went away is counted as skipped, an unpublished one is ignored, and errors from reaches and emit stop the walk.
- `GOFLAGS=-mod=mod just ci` passes.

**agent:claude-code/dae09bda** at 2026-10-01T07:12:46Z

PR #177 (https://git.local.sothr.com/terva-sh/lampi/pulls/177), stacked on #175 and #173. terva-review dispatched.
