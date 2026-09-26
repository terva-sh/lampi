# Browser metadata API

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
measure upload throughput. No endpoint returns transcript bodies or initiates
normalization, export, deletion, merge, or ingestion.
