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
| `/activity` | Accepted head updates per UTC hour or day; see below |
| `/operations` | Disk use, growth, queues, the serve process and machine freshness; see below |

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
measure upload throughput. Only the events, search, excerpt and raw artifact
routes below return transcript text.
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

The dashboard attempts at most 5 mints at once and one more every 12
seconds. Past that a mint is `429 rate_limited`. A request refused for
its input (name, expiry, profile) does not count; one that reaches the
public URL check does, whether or not it ends in a code.

| Refusal | Status and `error` |
|---|---|
| A name that is not a device name | `400 invalid_name` |
| An expiry that does not parse, or is over 30 days | `400 invalid_expiry` |
| A profile the lake does not hold | `400 unknown_profile` |
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

## Raw artifacts

Admins can download the bytes an agent uploaded for a session. The
browser routes need the `admin` role (see [web-dashboard.md](web-dashboard.md)).
Operators and viewers get `404`. They are browser routes, not under
`/api/web/v1`, and they exist only when serve wires in the blob store.

| Route | Result |
|---|---|
| `GET /sessions/{uid}/raw` | An HTML page listing the session's current artifacts with download links. No query parameters. |
| `GET /sessions/{uid}/raw/{sha256}` | One artifact's bytes. No query parameters. |

A download names a digest the session links to, current or earlier. Any
other digest is `404 not_found`, the same answer as a session that is not
there, so the route cannot probe the blob store.

The response is `application/octet-stream` with `Content-Disposition:
attachment`, `Accept-Ranges: bytes`, `Cache-Control: no-store` and
`X-Content-Type-Options: nosniff`. It carries at most 8 MiB:

- With no `Range` header, an artifact up to 8 MiB is a `200`. A larger
  one is a `206` with its first 8 MiB, a `Content-Range`, and a
  `Lampi-Raw-Truncated` header holding the full size.
- One `Range` of `bytes=a-b`, `bytes=a-` or `bytes=-n` is a `206`. A range
  longer than 8 MiB stops there and carries `Lampi-Raw-Truncated`.
- Several ranges, in one `Range` field or in several, another unit, or a
  range past the end is `416 invalid_range` with `Content-Range: bytes */SIZE`.
- `HEAD` answers with the same headers and no body.

The lake reads the whole response from the blob store first, then queues
an `artifact.read` audit event naming the admin, the session, the digest
and the byte range, then sends it. A read that fails is
`500 read_failed` and records nothing. A read whose event cannot be queued
is `500 audit_failed` and sends nothing. `HEAD` sends no bytes and records
nothing.

### Read tokens

A tool with no browser session reads the same artifacts with a read token
an admin minted (see [web-dashboard.md](web-dashboard.md#read-tokens)):

```sh
curl -fsS -H "Authorization: Bearer $(cat token-file)" -o artifact \
  https://lake.example/api/raw/v1/sessions/SESSION_UID/artifacts/SHA256
```

| Route | Result |
|---|---|
| `GET /api/raw/v1/sessions/{uid}/artifacts/{sha256}` | One artifact's bytes, exactly as the browser route above answers, cap, `Range` and `HEAD` included. |

The token goes in `Authorization: Bearer`. A missing, unknown, expired or
revoked token is `401 not_authenticated` with a `WWW-Authenticate: Bearer`
header. A session outside the token's scope is `404 not_found`, the same as
a session that is not there. A lake that cannot look the token up answers
`500 read_failed`, so a tool does not drop a token that is still good. The audit event's actor is `token:ID (LABEL)`.

A read token authenticates this route and nothing else. A browser
session cookie does not reach it, a read token does not reach any other
route, including `/api/web/v1` and `/v1`, and a device token does not
reach this one.

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

## Activity

`GET /api/web/v1/activity` counts accepted session head updates in UTC
buckets. The lake records one update each time a session head changes: a new
session, a transcript that grew, or a snapshot export rewritten. A repost of
bytes the lake already has, a stale post and a divergent copy change no head
and are not counted. See [architecture.md](architecture.md) for what is
recorded.

| Parameter | Values | Default |
|---|---|---|
| `bucket` | `hour` or `day` | `day` |
| `from` | RFC3339 time | `until` minus 24 hours (`hour`) or 7 days (`day`) |
| `until` | RFC3339 time | the end of the current bucket |
| `harness` | one harness name | every harness |

Each parameter appears at most once, and an empty value takes the default.
`from` is rounded down and `until` up to bucket boundaries in UTC. An `until`
later than the end of the current bucket is clamped to it. After rounding, the
range must be at most 14 days for `hour` and 90 days for `day`. A wider range,
an empty or reversed pair of times (checked before rounding, so two times inside
one bucket do not become that bucket), a time before 1970 or after 2200, an
unknown parameter, and a malformed time are `400 invalid_filters_or_cursor`; the
range is never silently narrowed. Because
the cap counts whole buckets, "the last 90 days" means `from` at a midnight.

```json
{
  "bucket": "day",
  "from": "2026-09-22T00:00:00Z",
  "until": "2026-09-29T00:00:00Z",
  "harness": "",
  "coverage_since": "2026-09-27T18:04:11.52Z",
  "as_of": "2026-09-28T13:45:10.1Z",
  "units": {"updates": "...", "net_logical_bytes": "..."},
  "totals": {"updates": 41, "net_logical_bytes": 5230118},
  "buckets": [
    {"start": "2026-09-22T00:00:00Z", "coverage": "none", "updates": null, "net_logical_bytes": null},
    {"start": "2026-09-27T00:00:00Z", "coverage": "partial", "updates": 12, "net_logical_bytes": 880412},
    {"start": "2026-09-28T00:00:00Z", "coverage": "full", "updates": 29, "net_logical_bytes": 4349706}
  ]
}
```

The example shortens `buckets`; a response has one entry for every bucket in the
range, in order.

- `updates` is the number of accepted head updates that began in the bucket.
- `net_logical_bytes` is the sum, over those updates, of the new head's logical
  size minus the old head's; a new session's old size is zero. It is negative
  when rewrites shrank heads. It is not network traffic, and it is not disk
  growth: the CAS stores each blob once and a grown transcript keeps its
  earlier blob.
- `coverage` compares the bucket with `coverage_since`, the time this catalog
  began recording. `none` buckets ended before that, were not measured, and
  carry `null` counts; they are not zero. `partial` holds the start of
  recording. `full` buckets were measured throughout. A catalog with no
  recorded start reports every bucket as `none` and `coverage_since` as `null`.
- `totals` sums the `partial` and `full` buckets.

Purging a session deletes its updates, so past buckets drop them. Restoring a
backup rewinds the history to the backup, and agents that upload again after the
restore are counted at the time the lake accepts them.

## Operations

`GET /api/web/v1/operations` reports what running the lake costs and whether
it keeps up. It takes one optional parameter, `range`, which is `7d` (hourly
buckets, the default), `30d` or `90d` (daily buckets). As on Activity, an
empty value takes the default. Any other value, a repeated `range`, or any
other parameter is `400 invalid_filters_or_cursor`.

```json
{
  "as_of": "2026-09-28T13:45:10.1Z",
  "process": {"version": "v0.1.2 (8fea3abdf607)", "started": "2026-09-28T09:01:00Z",
              "uptime_seconds": 17050, "schema_version": 11, "lake_id": "lake_..."},
  "storage": {
    "sampled_at": "2026-09-28T13:01:00Z",
    "components": {"cas": {"bytes": 7340032, "files": 8560}, "catalog": {"bytes": 4194304, "files": 2}},
    "total_bytes": 15728640,
    "filesystem": {"total": 107374182400, "free": 53687091200},
    "referenced": {"bytes": 9437184, "files": 9120},
    "unique": {"bytes": 6291456, "files": 8560}
  },
  "growth": {"range": "7d", "bucket": "hour", "points": [
    {"start": "2026-09-21T14:00:00Z", "total_bytes": null, "free_bytes": null},
    {"start": "2026-09-28T13:00:00Z", "total_bytes": 15728640, "free_bytes": 53687091200}
  ]},
  "queues": {"audit_pending": 0, "normalize_pending": 0, "normalize_failed": 2,
             "search": {"ready_sessions": 91, "indexed_sessions": 91, "behind_sessions": 0, "failed_sessions": 0, "last_reconcile": "..."},
             "upload_files": 0, "upload_bytes": 0},
  "machines": [
    {"device": "laptop", "state": "active", "source": "registration", "profile": "default",
     "machine_id": "01J...", "last_contact": "2026-09-28T13:44:02Z", "last_data": "2026-09-28T12:10:40Z",
     "freshness": "active", "sessions": 40, "updates_24h": 17}
  ]
}
```

The example shortens `components`, `points` and `machines`.

- **`storage`** is the newest sample serve took. Serve samples when it starts
  and then hourly, so the figures can be up to an hour old; `sampled_at` says
  when. Before the first sample, `storage` is `{}`.
  - `components` holds all eight parts of the lake directory: `cas`, `uploads`,
    `catalog`, `normalized`, `parquet`, `search`, `audit` and `other`. Bytes
    are allocated disk blocks, as `du` counts them, including directories.
    `files` counts regular files.
  - `total_bytes` is their sum.
  - `filesystem` is the capacity of the filesystem that holds the lake. It is
    absent where the platform does not report one.
  - `referenced` is the logical bytes of every artifact row, and `unique` is
    those of each distinct digest counted once. Versions of a growing file are
    distinct digests that share stored bytes, so `unique` is not what the CAS
    holds; `components.cas` is.
  - `current` is the logical bytes of each path's current version: the raw
    files the machines hold. `current_unique` counts each of those digests
    once, so `current` minus `current_unique` is files that are byte-for-byte
    copies of another. The page's compression against raw is `current`
    divided by `components.cas`. Both are absent from samples taken before
    serve measured them.
- **`growth`** has one point per bucket in the range. Each point carries the
  last sample taken in that bucket. A bucket with no sample is `null`, and a
  `null` means not measured, not zero.
- **`queues`**
  - `audit_pending` is audit events committed to the catalog but not yet in
    `audit.jsonl`.
  - `normalize_pending` and `normalize_failed` count sessions in those states.
  - `search` is the index coverage, and is absent when search is off.
  - `upload_files` and `upload_bytes` come from the newest sample.
- **`machines`** lists every device, and every machine that posted without a
  device bound to it, which has no `device` or `device_id` field. Revoked devices come last;
  the rest are ordered by how recently each was heard from.
  - `last_contact` is the device's last authenticated request since `started`.
    A restart clears it.
  - `last_data` is the newest upload the lake had not seen before.
  - `freshness` is `active` within a day of the later of the two, `idle`
    within a week, `quiet` after that, and `never` when neither is known.
  - `updates_24h` counts head updates in the last 24 hours.

## Devices

`GET /api/web/v1/devices` lists every device with the newest report its agent
sent, compared with the lake. It takes no parameters; any parameter is
`400 invalid_filters_or_cursor`. The `/devices` page shows the same data.

```json
{
  "as_of": "2026-09-28T15:02:00Z",
  "lake_release": "v0.2.0",
  "behind": 1,
  "local_rules": 0,
  "devices": [
    {"id": "dev_...", "name": "tehbeast", "state": "active", "source": "registration",
     "profile": "default", "machine_id": "01M3...", "created": "2026-09-28T14:18:50Z",
     "last_contact": "2026-09-28T15:01:40Z", "last_data": "2026-09-28T14:40:02Z", "freshness": "active",
     "reported": "2026-09-28T15:01:40Z", "agent_version": "v0.1.3", "version_state": "behind",
     "applied_version": "sha256:8427...", "current_version": "sha256:8427...", "profile_state": "current",
     "allow_source": "lake default", "deny_source": "none",
     "last_sync": {"at": "2026-09-28T15:01:38Z", "uploaded": 12, "manifests": 4, "refused": 3, "quarantined": 0, "unchanged": 202}}
  ]
}
```

- `version_state` compares `agent_version` with `lake_release`: `current`,
  `behind` or `ahead`. It is `unstamped` for an agent that is not a release
  build, whatever the lake is, since that says something about the agent
  alone. It is `unknown` when the device has sent no report, or when the agent
  is a release build and the lake is not. `behind` counts active devices that are behind.
- `profile_state` is `current` when the agent applied the profile version the
  lake would serve it now, `stale` when it applied another, and `unknown` when
  it has not said. It is `missing` if the device names a profile the lake does
  not hold, which a delete refuses, so it means the catalog is out of step.
  `current_version` is what the lake would serve.
- `advisory` is present when the agent's release matches one of the
  advisories this lake ships with (`internal/advisory/agents.json`):
  `severity` (`upgrade` or `urgent`), `reason`, and `fixed` and `link` when
  known. `urgent` lists the active devices that match an urgent advisory; it
  is empty, not absent, when none does.
- `allow_source` is where the agent's allow rules come from. `local` means the
  machine's `config.json` sets them and the lake's profile does not decide what
  it uploads. `local_rules` counts active devices like that.
- `no_profile` is true for a `token-file` device whose agent reports no
  profile: it has not pinned the lake, so no profile reaches it.
  `terva-lampi lakes adopt` on that machine pins it.
- `last_sync`, `last_error` and the agent fields are absent until the device
  sends a report. `last_contact` is the newest request, or the newest report
  from before serve started.
- Devices are ordered active first, then by the newest contact or data.

### One device

`GET /api/web/v1/devices/{id}` is one device by its `dev_` id: `device` is its
row from the list above, and `inventory` is the newest inventory its agent
sent, or `null` before it sends one. It takes no parameters. An id no device
has is `404 not_found`. The `/devices/{id}` page shows the same data, and takes
`?show=refused` to list the refused projects alone.

```json
{
  "as_of": "2026-09-28T15:02:00Z",
  "device": {"id": "dev_...", "name": "tehbeast", "state": "active", "...": "..."},
  "inventory": {
    "mode": "sociable", "generated_at": "2026-09-28T14:41:10Z", "received_at": "2026-09-28T14:41:11Z",
    "allowed": 1, "refused": 1, "refused_sessions": 221, "refused_bytes": 9437184,
    "projects": [
      {"git_remote": "github.com/acme/app", "cwd": "/home/me/src/app", "cwds": 2,
       "cwd_hash": "8427a42989cbc15f", "harnesses": ["claude"], "sessions": 12,
       "bytes": 409600, "newest": "2026-09-28T14:40:02Z", "allowed": true},
      {"cwd": "/home/me/scratch", "cwds": 1, "harnesses": ["codex"], "sessions": 221,
       "bytes": 9437184, "allowed": false, "reason": "no allow rule matches"}
    ]
  }
}
```

The projects are as the agent sent them; see
[protocol.md](protocol.md#post-v1agentinventory). `allowed` and `refused`
count them. A `strict` device lists allowed projects only, so its `refused` is
0 and `refused_sessions` and `refused_bytes` are all it says of the rest.
`truncated` is set when the agent held more projects than it could list.

### Device actions

Operators change a device by its `dev_` id, as `serve devices` does. The
rules of [registration codes](#registration-codes) hold: the `operator` role,
`404 not_found` for anyone else, POST with the `X-Lampi-CSRF` header, and
`403 csrf_failed` without it.

| Route under `/api/web/v1` | Result |
|---|---|
| `POST /devices/{id}/revoke` | Revokes the device. Its token stops on its next request. Final. |
| `POST /devices/{id}/unbind` | Clears the machine it is bound to; its next upload binds it again. |
| `POST /devices/{id}/profile` | Takes `{"profile": NAME}` and sets the profile its agent fetches. `default` goes back to the default. |

`revoke` and `unbind` take no body, or an empty object. Each answers `200` with
`{device: {id, name, state, profile, machine_id}}`.

| Refusal | Status and `error` |
|---|---|
| No device has the id, or the action is not one of these | `404 not_found` |
| A profile the lake does not hold | `400 unknown_profile` |
| A body that is not one JSON object of these fields, or no profile | `400 invalid_request` |
| The device is revoked | `409 revoked`, with the device |
| Unbinding a device bound to no machine | `409 not_bound`, with the device |

Each change goes to `audit.jsonl` with the operator as actor. A change whose
audit line fails still stands, and answers `500 audit_failed` with the device;
the line stays queued and is written at the next flush.

## Review queue

`GET /api/web/v1/review` is the review queue: every refused project in the
newest inventories of the active devices, grouped lake-wide by project. A
project is named by its folded git remote, or by its cwd when it has none, so
the same repository on two devices is one entry. The `/review` page shows the
same data. Any viewer can read it.

It takes `device` (a `dev_` id), `harness` and `profile`, each at most once, to
narrow the queue; an empty value means all. Anything else is
`400 invalid_filters_or_cursor`.

```json
{
  "as_of": "2026-09-28T15:02:00Z",
  "sightings_since": "2026-09-28T12:00:00Z",
  "filter": {},
  "needs_review": [
    {"key": {"kind": "git_remote", "key": "github.com/acme/app"},
     "devices": [
       {"device_id": "dev_...", "device_name": "desk", "profile": "ci", "state": "needs_review",
        "first_seen": "2026-09-28T14:41:11Z",
        "project": {"git_remote": "github.com/acme/app", "cwd": "/src/app", "cwds": 1,
                    "harnesses": ["claude"], "sessions": 12, "bytes": 409600,
                    "allowed": false, "reason": "no allow rule matches"}}],
     "sessions": 12, "bytes": 409600, "first_seen": "2026-09-28T14:41:11Z"}
  ],
  "allow_pending": [],
  "denied": [],
  "hidden": [{"key": {"kind": "cwd", "key": "/home/me/scratch"}, "hidden_by": "oidc:...",
              "hidden_at": "2026-09-28T14:50:00Z", "note": "throwaway", "devices": 1}],
  "strict": [{"device_id": "dev_...", "device_name": "locked", "sessions": 3, "bytes": 12288}]
}
```

- Each device's copy of a project has a `state`, and a project is listed in the
  section of each state its copies are in:
  - `needs_review`: refused, not hidden, and the device's profile has no rule
    that allows it.
  - `allow_pending`: the device's profile allows it now, and the device has not
    sent an inventory since. A device with `local_allow` is never allow
    pending, since its profile's allow rules do not reach it. A device that
    applied its current profile and sent an inventory after that profile
    was saved is not waiting either: its copy is `needs_review` with
    `still_refused` set.
  - `denied`: a deny rule refuses it, or it has no cwd, so no allow rule can let
    it through.
- `profile` is the profile the device fetches. `local_allow` marks a device
  whose `config.json` sets its own allow rules, which a profile rule does not
  reach. `no_profile` marks one that fetches no profile at all, as on the
  devices route; it is also `local_allow`.
- `first_seen` is when the lake first saw the project on the device.
  `first_seen_at_or_before` marks a project first seen when the lake began
  recording sightings, at `sightings_since`: it may have been there before.
- A hidden project leaves the other sections and is listed in `hidden`, with
  how many active devices refuse it now.
- A `strict` device names no refused project; `strict` gives its totals.
- Sections are ordered by `first_seen`, newest first.

### Hiding projects

Operators hide projects they will not import, and unhide them. The rules of
[registration codes](#registration-codes) hold: the `operator` role,
`404 not_found` for anyone else, POST with the `X-Lampi-CSRF` header, and
`403 csrf_failed` without it.

| Route under `/api/web/v1` | Body | Result |
|---|---|---|
| `POST /review/hide` | `{"keys": [{"kind": "git_remote", "key": "github.com/acme/app"}], "note": "vendored"}` | Hides each key. `note` is optional, at most 500 characters. |
| `POST /review/unhide` | `{"keys": [...]}` | Removes each key's hide. It records no note: an empty or blank `note` is ignored, and any other is refused. |

A key is a project as the [review queue](#review-queue) names it: `kind` is
`git_remote` or `cwd`. Each answers `200` with `{"changed": [KEY, ...]}`, the
keys it hid or unhid. A key already in that state is left alone, so a repeat
changes nothing, and a hide keeps its first note. One request takes 1 to 500
keys, and changes all of them or none.

| Refusal | Status and `error` |
|---|---|
| Not one JSON object of these fields, no keys, more than 500, a key that is not a key, or a note that is not blank on unhide | `400 invalid_request` |
| A note over 500 characters | `400 invalid_note` |

A hide sends nothing to agents. Each change goes to `audit.jsonl` as
`project.hidden` or `project.unhidden` with the operator as actor. A change
whose audit line fails still stands, and answers `500 audit_failed` with
`changed`.

### Allowing projects

Allow selected, as the browser API. The same operator and CSRF rules hold.

`POST /api/web/v1/review/allow` plans. It takes
`{"keys": [KEY, ...], "device": "dev_...", "width": "repository"}`:

- `device` is optional and narrows the plan to that device's copies.
- `width` is optional. `repository`, the default, adds a `git_remote` rule per
  repository. `owner` adds one `git_remote_prefix` per owner instead, except
  where the owner would be the bare host. A folder always gets `cwd_prefix`.
  Any other value is `400 invalid_request`.

It saves nothing and answers `200`:

```json
{
  "profiles": [
    {"name": "default", "base_revision": 7, "stored": true,
     "rules": [{"rule": {"git_remote_prefix": "github.com/acme"},
                "key": {"kind": "git_remote", "key": "github.com/acme/app"},
                "for_devices": ["laptop"]}],
     "removed": [{"git_remote": "github.com/acme/lib"}],
     "reach": {"admits": [{"key": {"kind": "git_remote", "key": "github.com/acme/app"},
                           "devices": ["laptop"], "sessions": 12}],
               "drops": [], "unlisted": []},
     "document": {"projects": {"allow": ["..."]}},
     "devices": [{"id": "dev_...", "name": "laptop"}], "local_allow": 0}
  ],
  "skipped": [{"kind": "cwd", "key": "/home/me/done"}]
}
```

`skipped` lists keys that no longer need review. `key` on a rule is the first
selected project it is for. At owner width, one rule can be for several.
`removed` lists the allow rules the profile had that the new rules cover,
which the document leaves out. `reach` lists the projects in the devices'
newest inventories that the change admits and drops. `unlisted` names the
devices that send no list of refused projects. See
[Editing a profile](web-dashboard.md#editing-a-profile). `POST
/api/web/v1/review/allow/save` saves the plan with
`{"profiles": [{"name", "base_revision", "document"}], "note": "..."}`. It
saves every profile or none, and answers `200` with
`{"profiles": [{"name", "version", "revision"}]}`.

| Refusal | Status and `error` |
|---|---|
| Not one JSON object of these fields, no keys or profiles, or more than 500 | `400 invalid_request` |
| A document the agent would refuse | `400 invalid_profile`, with `message` |
| A note over 500 characters | `400 invalid_note` |
| A profile moved since `base_revision`; nothing is saved | `409 changed` |

Each saved profile gets its own revision with the note, and a `profile.put`
line in `audit.jsonl`.

## Profiles

`GET /api/web/v1/profiles` and `GET /api/web/v1/profiles/{name}` read the
lake's profiles. Viewers can read them. Neither takes a query parameter.

The list is `{as_of, profiles, ignored_files}`. Each profile has `name`,
`version`, `revision`, `stored`, `updated`, `updated_by` and `devices`, the
count of active devices that fetch it. `stored` is false for a default no
one has saved, which the lake serves empty. The default comes first.

One profile adds:

- `document`, the profile agents receive;
- `harnesses`, one `{id, set, enabled}` per harness. `set` is false when the
  profile leaves the harness as the agent has it;
- `device_list`, the active devices that fetch it, as `{id, name,
  allow_source}`;
- `revisions`, up to 50 of them, newest first, as `{id, version, note,
  deleted, created, created_by}`.

A name that is not a profile is `404 not_found`, with `latest_revision`: the
newest revision in its history, a deletion included, or `0`. `ignored_files` lists each
profiles file serve does not read, as `{path, import}`, where `import` is
the command that brings it into the catalog. It is empty, not absent, when
there is none.

### Saving and deleting profiles

Operators write profiles with the same rules as
[device actions](#device-actions): the `operator` role, `404 not_found` for
anyone else, and the `X-Lampi-CSRF` header.

- `PUT /api/web/v1/profiles/{name}` takes
  `{"document": {...}, "base_revision": N, "note": "..."}`.
  - `document` is the whole profile.
  - `base_revision` is the `revision` you read. For a name that is not stored
    it is the `latest_revision` its `GET` answers alongside `404`: `0` for a
    name never saved, else the deletion that removed it.
  - Rules are stored with `git_remote` and `git_remote_prefix` folded as the
    agent compares them.
  - It answers `200` with `{profile: {name, version, revision}}`. That is also
    the answer when the document is what is already saved.
- `POST /api/web/v1/profiles/{name}/rollback` takes `{"revision": R,
  "base_revision": N, "note": "..."}`. It saves revision R's document again as
  a new revision noted `rollback to revision R`, and answers like a PUT. A
  revision that is not this profile's is `404 not_found`, and one that records
  a deletion is `400 deleted_revision`.
- `DELETE /api/web/v1/profiles/{name}` takes `{"base_revision": N, "note":
  "..."}` and answers `204`.

| Refusal | Status and `error` |
|---|---|
| Another save or a delete landed after `base_revision` | `409 changed`, with the saved profile on a PUT |
| A document an agent would refuse, such as one that sets a harness root, or over 500 allow or deny rules | `400 invalid_profile`, with `message` |
| A body that is not one object of these fields, or no `base_revision` | `400 invalid_request` |
| A name that is not a profile name | `400 invalid_name` |
| A note over 500 characters | `400 invalid_note` |
| Deleting the default profile | `409 default` |
| Deleting a profile an active device uses | `409 in_use` |

Each write goes to `audit.jsonl` with the operator as actor. `profile.put`
names the parts that changed (`projects.allow`, `projects.deny`, `harnesses`,
`agent`), and the revisions hold both documents. A write whose audit line
fails still stands, and answers `500 audit_failed`.
