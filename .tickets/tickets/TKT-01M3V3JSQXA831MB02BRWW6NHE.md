---
schema: 3
id: TKT-01M3V3JSQXA831MB02BRWW6NHE
title: "Read API: stream filtered normalized events to read tokens"
type: task
status: ready
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
claim: null
archive: null
created_at: 2026-10-01T06:49:12Z
updated_at: 2026-10-01T06:49:32Z
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

- [ ] The same filters and fields return the same rows as export on the same synthetic lake.
- [ ] Only sessions in the token's scope are read; tokens without events:read, device tokens and cookies are refused.
- [ ] A request with no filter is refused; a cut stream is distinguishable from a complete one, as documented.
- [ ] Each request writes one events.read audit event with token, filters and row count and no content.
- [ ] docs/web-api.md documents the route.
