# Browser API

Reference for the routes behind the browser dashboard. To turn the dashboard on,
see [web-dashboard.md](web-dashboard.md). Back to the
[documentation index](README.md).

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
| `/sessions/{uid}/excerpt` | A span of events as paste-ready text; see below |

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
measure upload throughput. Only the events, search and excerpt routes below
return transcript text.
No endpoint initiates normalization, export, deletion, merge, or ingestion.
The registration routes below are the only writes, and only operators reach them.

## Registration codes

Operators can mint, list and cancel registration codes, the same codes
`serve register` makes on the lake host. These routes need the
`operator` role (see [web-dashboard.md](web-dashboard.md)). Anyone else,
viewers included, gets `404 not_found`, so a viewer learns nothing about
them. Writes are POST and carry the session's CSRF token in the
`X-Lampi-CSRF` header. A write from another origin, or without the
token, is `403 csrf_failed`.

| Route under `/api/web/v1` | Result |
|---|---|
| `GET /registrations` | Every code, newest first, as `{items: [], as_of}`. No query parameters. |
| `POST /registrations` | Mints a code. `201` with the code, once. |
| `POST /registrations/{id}/revoke` | Cancels a pending code by its `reg_` id. |

A listed code has `id`, `name`, `state` (`pending`, `used`, `expired` or
`revoked`), `profile`, `created`, `expires` and `created_by`, then
`revoked` and `revoked_by` once cancelled, and `used`, `device_id` and
`device_name` once redeemed. `created_by` is `cli` for `serve register`,
`web:SUBJECT (DISPLAY NAME)` for the dashboard, and empty for a code
minted before it was recorded. The list never holds a code's secret.
Listing writes the expiries since the last look to `audit.jsonl`, as
`serve register --list` does.

A mint takes a JSON body `{name, profile, expires}`. `name` is the
device name: lowercase letters, digits, `.`, `-` and `_`. `profile`
defaults to `default`. `expires` is a duration such as `1h` or `72h`,
default `1h`, at most 30 days. Minting also needs a sign-in at the IdP
in the last 10 minutes; otherwise it is `403 fresh_login_required` with
a `login` URL that signs in again. The response holds the code in
`code`, the lake's `lake_id` and key `fingerprint`, and
`install_command`, a line that installs terva-lampi and registers the
machine:

```sh
 curl -fsSL https://raw.githubusercontent.com/terva-sh/lampi/TAG/install.sh | TERVA_LAMPI_CODE='CODE' sh -s -- --version TAG --register --fingerprint SHA256:…
```

`TAG` is the release the lake was built from, so the machine installs
the lake's version, and `install_pinned` is true. A lake built from no
release tag gives a line that fetches `install.sh` from `main` and
installs the latest release, and `install_pinned` is false. The line
begins with a space so shells that skip such lines leave it out of
history. The code is not shown again: only its hash is stored.

The dashboard mints at most 5 codes at once and one more every 12
seconds. Past that a mint is `429 rate_limited`.

| Refusal | Status and `error` |
|---|---|
| A name that is not a device name | `400 invalid_name` |
| An expiry that does not parse, or is over 30 days | `400 invalid_expiry` |
| A profile not in `profiles.json` | `400 unknown_profile` |
| A body that is not one JSON object of these fields | `400 invalid_request` |
| A device, or a pending code, already has the name | `409 name_taken` |
| No identity, no public URL, or a public URL that does not reach this lake | `503 lake_not_ready` |
| Cancelling a code that does not exist | `404 not_found` |
| Cancelling a code that was used | `409 already_used` |
| Cancelling a code that was already cancelled | `409 already_revoked`, with the code |

Each mint and each cancel goes to `audit.jsonl` with the operator as
actor, and to the catalog as `created_by` or `revoked_by`. A cancel whose
audit line fails still stands, and answers `500 audit_failed` with the
code.

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
| `q` | 3 characters to 1024 bytes of UTF-8, no NUL. It matches as a case-insensitive substring of `content_text`. Quotes, `OR`, `NEAR`, `*` and `:` are literal text, not syntax. |
| `harness`, `project`, `unlinked` | As on `/sessions`. An empty value means no filter. |
| `since`, `until` | Recorded time in UTC, as RFC 3339 or `YYYY-MM-DD`. `since` is inclusive and `until` exclusive; a date-only `until` covers that whole day. With either set, events with no recorded time are left out. `since` must be before `until`. |
| `event_type` | Exact: `message`, `tool_call`, `tool_result`, `usage`, `compaction`, `meta`, `error`, `unknown`, or `unreadable` for a line the index could not decode. |
| `actor` | Exact: `user`, `assistant`, `system`, `tool`, `harness`. |
| `tool` | Exact tool name, case-sensitive, up to 256 bytes. |
| `tool_error` | `true` or `false`, matching the recorded flag. A result whose harness did not record it matches neither. |
| `raw_type` | Exact harness record type, up to 256 bytes. |
| `limit` | 1–200, default 50. |
| `cursor` | Continues from `next_cursor`. It is bound to the other parameters. |

`q` may be left out when at least one event filter (`event_type`, `actor`,
`tool`, `tool_error`, `raw_type`) is set, for questions such as every failed
tool call in one project. Session filters alone are refused: that would list
the corpus, and `/sessions` already does. Without `q`, the snippet is the start
of the event's text and `match_len` is 0. A filter on `actor` alone walks events
newest first until the page fills; the other event filters use an index.

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
| 400 | `invalid_request` | No text and no event filter, short query, unknown filter value, bad date, unknown or repeated parameter, bad cursor |
| 503 | `search_unavailable` | The lake has no search index |

## Excerpts

`/sessions/{uid}/excerpt` renders a span of events as plain text, ready to paste
into a new agent session. The viewer's Copy button and the plain page
`/sessions/{uid}/excerpt` (outside `/api`, served as `text/plain`) return the same
text. Parameters, each at most once: `from` (default 0), `count` (1–200, default
200) and `gen` (pins the generation).

The response is `{session_uid, generation, from, to, events, truncated, link,
text}`. `to` is one past the last event included. `link` is the absolute viewer
URL of the first event, built from the configured `base_url`. `text` opens with
a header naming the harness, session, project, span and link. Each event follows
as `[#position actor type tool (error) recorded_at]` and then its text. The
excerpt keeps at most 64 KiB of one event's text and says where it cut. Encrypted
values are replaced by a line saying they were omitted. When the text would pass
512 KiB, the span stops early and `truncated` is true; the text says where it
ended.

Copy-out needs only the viewer role. It returns text the viewer can already read
on the transcript page. Bulk downloads and training formats are the separate,
unimplemented export feature (TKT-01M3F2PGR), which needs its own role and
project policy. Errors match the events route; a span that starts past the end is
`400 invalid_request`.
