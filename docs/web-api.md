# Browser API

The optional browser interface is enabled by `serve --web-config PATH`. It
requires OIDC viewer membership and a browser session. Device bearer tokens
cannot authorize it; browser cookies cannot authorize `/v1`. Without web config,
these routes do not exist. `/v1` and `/healthz` retain their original contracts.

All reads are GET, return JSON and `Cache-Control: no-store`, and have a five-second
catalog-query budget. Unauthenticated reads return `401 not_authenticated`,
unmapped session identities `403 not_authorized`, unknown sessions `404 not_found`,
bad input `400 invalid_filters_or_cursor`, and unavailable reads `503 read_unavailable`.
Other catalog failures are `500 read_failed`. Detailed normalization errors and
raw manifests never appear in these responses.

| Route under `/api/web/v1` | Result |
|---|---|
| `/overview` | Sessions, artifact rows, contributing machines, divergent artifacts, harness counts, normalization counts, `as_of` |
| `/sessions` | Session summary page |
| `/sessions/{uid}` | One session summary |
| `/sessions/{uid}/artifacts` | Artifact metadata page; `current=true` selects current artifacts |
| `/sessions/{uid}/provenance` | Machine/digest/path observation page |
| `/sessions/{uid}/conflicts` | Divergent artifact page |
| `/conflicts` | Divergent artifacts across sessions |
| `/sessions/{uid}/events` | One page of the session's published normalized events; see below |
| `/search` | Literal text search over indexed events; see below |

Lists use `{items: [], next_cursor: "", as_of: "UTC timestamp"}`. Empty lists are
arrays. `limit` defaults to 50 and accepts 1–200. `cursor` is opaque, bound to the
collection, session and filters including page size; never construct or edit it.
An empty next cursor means the final page. Session ordering is latest head update
first, then session UID descending. These are live views: concurrent ingestion
can move rows, so restart pagination to refresh the list.

Session filters are exact `harness`, exact `project`, `unlinked=true` (mutually
exclusive with project), and `state=pending|failed|ready|unknown`. Unknown or
repeated parameters, invalid limits/cursors and unsupported filter values are
refused. Other collections accept only limit/cursor and the artifact current
selector. Detail and overview accept no query parameters.

A session summary contains `session_uid`, `native_session_id`, `harness`,
`project_id`, `project_label`, `head_sha256`, `last_head_update`,
`normalization_state`, `machines` and `machine_count`. Machines are a sorted
preview of at most five identifiers; provenance pages contain the rest.
Native IDs, paths and project labels are display previews capped at 512
characters; project IDs at 4096 and machine identifiers at 128. Full source
metadata is retained in the lake; these display limits do not rewrite it.

A queued job (including running/retrying) is pending. A recorded terminal error
is failed. Ready means a successful publication recorded both the current
normalization generation and current head digest. Otherwise status is unknown;
legacy rows are not assumed ready. This is publication metadata, not a filesystem
integrity probe. Later content readers must still detect missing files.

The overview counts catalog artifact versions, not unique blobs or disk bytes.
Contributing machines are not online machines. Head update timestamps do not
measure upload throughput. Only the events and search routes below return
transcript text.
No endpoint initiates normalization, export, deletion, merge, or ingestion.

## Transcript events

`/sessions/{uid}/events` reads the published normalized JSONL of one session.
It never starts normalization and never reads raw blobs. The reader lives in
`internal/recall`, the query layer the planned MCP server will share, so both
return the same shape.

Parameters, each at most once:

| Parameter | Meaning |
|---|---|
| `from` | First event position, counted from 0. Default 0. A position past the end returns an empty final page. |
| `limit` | Events per page, 1–200, default 100. |
| `cursor` | Continues from `next_cursor`. `from` is ignored when it is set. |
| `gen` | Pins the read to one generation. Generation 0 is valid. |

A page is `{session_uid, generation, head_sha256, from, items, next_cursor,
prev_from, end, as_of}`. `prev_from` is the `from` of the previous page, or null
on the first page. `end` is true when the page reaches the last event; then
`next_cursor` is empty.

Each item is `{position, link, content_bytes, content_truncated, extra_omitted,
opaque_content, oversized, unreadable, event}`. `event` is the normalized event
(schema version 1) with three reductions, each flagged:

- `content_text` longer than 32 KiB is cut at a rune boundary.
  `content_bytes` is the full length and `content_truncated` is true.
- Keys under `extra` that name encrypted, cipher or sealed values are removed.
  They are never decrypted. `opaque_content` is true.
- An `extra` object that still encodes to more than 16 KiB is dropped.
  `extra_omitted` is true.

A line longer than 16 MiB is `oversized` with a null event. A line that is not
JSON is `unreadable`. Both keep their position. A page stops at the limit or
once its items reach 1 MiB, whichever comes first, and always holds at least one
event. `link` is the viewer URL of that event in that generation.

Every page holds one generation. The reader reads the publication state, opens
the file, and reads the state again. A worker bumps the generation before it
replaces the file, so an unchanged state proves which generation the open file
holds. Cursors are signed with a key made at process start, and bound to the
session, generation, head and file identity. A restart invalidates them.

| Status | Error | Meaning |
|---|---|---|
| 400 | `invalid_request` | Bad parameter or cursor |
| 404 | `not_found` | Unknown session |
| 409 | `transcript_unavailable` | `state` is `pending`, `failed`, `unknown`, or `missing` (catalog says ready, file absent) |
| 409 | `generation_changed` | `gen` or the cursor names a generation that is no longer published; reload |

The events route is an authenticated read of transcript text with the viewer role.
Text is returned exactly as stored. The web page renders it as plain text.

## Search

`/search` matches text in the search index described in
[web-dashboard.md](web-dashboard.md#search-index). It uses the same
`internal/recall` query layer as the events route.

| Parameter | Meaning |
|---|---|
| `q` | Required. 3 characters to 1024 bytes of UTF-8, no NUL. It matches as a case-insensitive substring of `content_text`. Quotes, `OR`, `NEAR`, `*` and `:` are literal text, not syntax. |
| `harness`, `project`, `unlinked` | As on `/sessions`. An empty value means no filter. |
| `since`, `until` | Recorded time in UTC, as RFC 3339 or `YYYY-MM-DD`. `since` is inclusive and `until` exclusive; a date-only `until` covers that whole day. With either set, events with no recorded time are left out. `since` must be before `until`. |
| `limit` | 1–200, default 50. |
| `cursor` | Continues from `next_cursor`. It is bound to the other parameters. |

A page is `{items, next_cursor, as_of, coverage}`. Hits are ordered newest
indexed first; a session that was re-normalized moves up. Each hit carries:

- `session_uid`, `native_session_id`, `project_label`, `generation`, `position`
  and `link`, the viewer URL of that event in that generation.
- `harness`, `project_id`, `event_type`, `actor`, `tool_name`, `tool_error` and
  `recorded_at`, which is null when the event has none.
- `snippet`, plain text around the first match with line breaks flattened, and
  `match_start`/`match_len`, byte offsets of the match within it. `match_len` is 0
  when the match could not be placed.

`coverage` is `{ready_sessions, indexed_sessions, behind_sessions,
failed_sessions, last_reconcile}`. Only sessions whose normalization is ready
are searchable. Every hit is re-checked against the catalog before it is
returned, so a pending, failed, superseded or purged session never appears,
even when the index has not caught up. A page can therefore hold fewer than
`limit` hits while `next_cursor` is set. Only the first 256 KiB of an event's
text is indexed.

| Status | Error | Meaning |
|---|---|---|
| 400 | `invalid_request` | Missing or short query, bad date, unknown or repeated parameter, bad cursor |
| 503 | `search_unavailable` | The lake has no search index |
