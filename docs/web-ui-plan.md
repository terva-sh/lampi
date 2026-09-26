# Web dashboard and retrieval plan

Status: release A implemented and validated; releases B/C remain draft. The owner approved a
small lake dashboard with OIDC in its first release and asked for executable
tickets on 2026-09-26. This document records the defaults for those tickets;
it does not claim a deployed endpoint or change the existing lake policy.

## Purpose and releases

The first release answers what has reached the lake, where it came from, and
whether it has normalized. Later releases let an authorized person read and
retrieve stored sessions, then measure ingestion over time.

| Release | Deliverable | Boundary |
|---|---|---|
| A | OIDC login, overview, filtered session list, metadata and conflicts | Read-only metadata; no transcript body, downloads, or administration |
| B | Transcript viewer, text search, selected JSONL downloads | Authorized retrieval; no arbitrary SQL or raw filesystem/CAS access |
| C | Recorded ingestion history and charts | Measurements collected after rollout; no invented historical throughput |

All work is tracked in the epic and child tickets linked at the end. Newly
filed tickets remain draft until promoted by the owner. Promotion of a release
authorizes its implementation, not deployment or identity-provider changes.
An agent can implement and validate against isolated synthetic data and a fake
HTTPS identity provider without production credentials or a live host.

## Existing foundation

`internal/api/server.go` serves health, stats, conflicts, and device-authenticated
ingestion. `internal/catalog` stores sessions, artifact versions, provenance,
project links, normalization generations and queued jobs. Normalizers produce
JSONL and Parquet for six harnesses. `internal/cli/export.go` implements local
events and allowlisted ShareGPT/trajectory exports. At planning time no browser UI or OIDC existed; release A now implements them.

The session `ingested_at` advances on a head change. Provenance records the
first observation of a session/machine/digest tuple, not every upload attempt.
Neither is a complete throughput history. Artifact sizes are logical sizes,
not unique CAS disk usage. Contributing machines are not online machines.
Missing usage values are unknown, not zero. A missing normalize error is not
proof of a successful projection.

## Architecture and alternatives

Serve Go `html/template` pages and embedded CSS/JavaScript in the existing Go
binary. Use a small same-origin JSON read API, shared catalog query functions,
and progressive enhancement. Use no CDN, frontend framework, Node build, or
separate web service for release A. Tables and forms work without JavaScript;
JavaScript adds refresh and simple charts. The browser never opens SQLite,
Parquet, CAS, or a provider token directly.

A React/Svelte frontend would help a much richer explorer, but its build and
dependency stack are unnecessary for the first dashboard. A separate service
adds a deployment and authentication boundary without a current need.
An OIDC proxy is viable but application OIDC matches the sibling repositories
and gives the read API explicit authorization. These alternatives can be
revisited when a concrete feature needs them.

Keep browser identity code in a separate package, such as `internal/webauth`,
and pages in `internal/web`. Existing `internal/auth` device tokens retain
their meaning. Compose handlers through the existing access-log, deadlines,
active-request accounting and graceful-shutdown path; avoid duplicate logs.

### Routes

| Route | Access and behavior |
|---|---|
| `/healthz` | Existing public process probe, no new catalog/auth details |
| `/v1/*` | Existing device-token protocol, unchanged response contracts |
| `/auth/oidc/start`, `/auth/oidc/callback` | OIDC flow, bounded login attempts |
| `POST /auth/oidc/logout` | Browser session revocation, same-origin CSRF check |
| `/`, `/sessions`, `/sessions/{uid}`, `/conflicts` | Viewer pages |
| `/api/web/v1/overview`, `/api/web/v1/sessions` | Viewer JSON reads |
| `/api/web/v1/sessions/{uid}` | Viewer session metadata |
| `/api/web/v1/conflicts` | Viewer paginated conflicts |
| `/assets/*` | Embedded static assets only; no lake content |

No web configuration means the new page, auth, and browser API routes are
disabled; existing ingestion behavior stays unchanged. When web is enabled,
require a nonempty device-token set even on loopback: a TLS proxy would otherwise
expose unauthenticated `/v1` operations. Browser cookies never authorize `/v1`;
device tokens never authorize the web read API. Guard APIs before resource
lookup. Unauthenticated page requests redirect to login, JSON requests return
JSON `401`, and authenticated users without the required role receive `403`.
No CORS grant is needed.

### Server configuration

Add an explicit `serve --web-config PATH` JSON file. Do not reuse or implicitly
load the uploading agent's `config.json`. Keys are `base_url` and `oidc` with
`issuer`, `client_id`, `client_secret_file` (optional for a public client),
`scopes`, `groups_claim`, and `role_map`. Reject unknown keys, missing required
values, unknown roles and an empty role map. Release A accepts only `viewer`.
Derive the callback from the configured base URL plus `/auth/oidc/callback`.
Base URL has an origin only, no path, query, userinfo or fragment. Production
requires HTTPS; an HTTP base URL is permitted only for loopback development.
Issuer and discovered authorization, token and JWKS endpoints require HTTPS.
Use the system trust store, with an injected client for test HTTPS providers.
Do not add an issuer-check or TLS-verification bypass.

Use snake_case names consistent with Terva. Defaults request `openid`,
`profile`, `email`, `groups`, deduplicated; the operator can override extra
scopes and groups claim. Secret contents are read from a file, never an argument,
example, log or API response. Configuration reload requires restart. Existing
SIGHUP remains device-token reload only. Invalid configuration fails before
opening the listener. An unreachable IdP leaves ingestion running and login
unavailable; bounded lazy discovery retries on a later login.

## OIDC precedents and decisions

Reviewed these source revisions on Forgejo, not running deployments:

- [Terva OIDC at 9450cdee4317a5759655726810de96cd30e2da9b](https://git.local.sothr.com/terva-sh/terva/src/commit/9450cdee4317a5759655726810de96cd30e2da9b/packages/agent/oidc/oidc.go)
  and its `packages/agent/web/oidc.go`: provider-neutral discovery (built
  against Authentik), PKCE, groups claim and group-to-role map. An unmapped
  user gets no role. Sessions are in memory with a twelve-hour lifetime.
- [Canvas provider at eebcbc066d233aa0d36dfa9ca647c64c2a2aaae4](https://git.local.sothr.com/terva-sh/git-ticket-canvas/src/commit/eebcbc066d233aa0d36dfa9ca647c64c2a2aaae4/internal/auth/provider.go)
  and `internal/auth/guard.go`, `sessions.go`: lazy discovery, opaque in-memory
  sessions, browser redirects versus API `401`, twelve-hour sliding idle expiry.
- [Ketju sessions at c62e6f777b712ecdfae74df99da7b2a7f9a41c2b](https://git.local.sothr.com/terva-sh/ketju/src/commit/c62e6f777b712ecdfae74df99da7b2a7f9a41c2b/internal/auth/session/session.go)
  and `internal/auth/oidc/oidc.go`, `internal/web/auth/auth.go`: hashed persistent
  session identifiers, idle and hard expiry, revocation, `__Host-` cookies and
  POST logout. Persistent sessions and multiple providers exceed release A.

Use `github.com/coreos/go-oidc/v3/oidc` and `golang.org/x/oauth2`, as all three
do. Adapt the patterns and tests rather than importing a sibling's internal
package or depending on the whole Terva application. If copying code, review
its license and preserve required attribution.

Authorization Code + S256 PKCE uses independent random state, nonce and verifier.
Bind attempts to an opaque browser cookie, expire after ten minutes, consume
once, and cap outstanding attempts. Verify signature, issuer, audience, expiry,
nonempty subject and nonce; pin asymmetric signing algorithms. Use `(issuer,
subject)` as identity, with email/name only for display. Map exact group names
from the configured claim to roles; malformed or unmapped groups grant nothing.

Keep session identities server-side with 256-bit opaque browser identifiers.
Use one-hour idle expiry and a twelve-hour absolute lifetime. Polling counts as
activity but cannot extend the absolute lifetime. Roles are a login snapshot;
membership changes at the IdP take effect at next login or hard expiry. A service
restart invalidates all sessions. Logout deletes the server record. No refresh
tokens or provider tokens are persisted. Persistent sessions and immediate IdP
revocation are deferred, not implied by this release.

Production cookies use `Secure`, `HttpOnly`, `Path=/`, no Domain and a
`__Host-lampi_` prefix. Use `SameSite=Lax` for the callback attempt and session.
Loopback HTTP development uses distinct unprefixed cookies. Return destinations
must be local absolute paths, rejecting protocol-relative paths, backslashes
and encoded equivalents. Logout requires a same-origin token/check; GET cannot
change session state. Bound auth network calls and in-memory state growth.

Escape all project paths, names, error messages and later transcript text. Use
external self-hosted scripts, a restrictive CSP, `nosniff`, no framing, and
`Cache-Control: no-store` on authenticated data and auth responses. Never log
cookies, tokens, client secrets, authorization codes, state, callback queries
or provider response bodies; emit categorized failures instead. Keep these
properties under tests, including malicious fixture content.

## Release A data and interface contract

Overview: counts of sessions, artifact rows, distinct contributing machine IDs,
normalization states and divergent artifacts, plus sessions grouped by harness.
Label artifact counts as rows/versions, not unique blobs. Counts use one read
snapshot; no CAS walk or transcript scan. No storage-size or throughput cards.

The session table shows UID/native ID, harness, project, contributing machine
IDs, last head update and normalization state. Unknown projects remain unknown,
not one synthetic project. Use `project_id` for grouping; display a safe label
from stored metadata. Do not expose raw manifest/unknown fields wholesale.
Metadata detail includes current artifacts, paginated history/provenance and
conflicts. Show safe failure categories; detailed diagnostics stay with operators.

Filters are exact harness, exact project ID (including an explicit unlinked
choice), and normalization state. JSON lists use `items`, `next_cursor` and
`as_of`; default page size 50, maximum 200. Validate filters/cursors and return
`400` for malformed input. Sort sessions newest head update first, UID as the
tie-breaker. Use keyset pagination with filters bound into the cursor, and indexes
for supported predicates/order. Order timestamps chronologically even when old
RFC3339Nano strings have different fractional precision. Lists are live views:
rows can move on ingest; refresh restarts pagination. Detail returns `404` for
an unknown UID. Every child collection must also be bounded.

Record an explicit nullable published normalization generation and head digest. Existing rows start
unknown unless safely reconciled; an empty error must never imply success.
State precedence: queued job (including running/retrying) is `pending`; a
recorded terminal failure is `failed`; published generation and head equal to the current
generation and head is `ready`; otherwise `unknown`. Publication records success only
after the matching generation's derived files are published. A superseded worker
must not mark a newer generation ready. Restart, failure, purge and migration
must preserve these meanings. Later content reads still detect missing files.

Poll overview and the first session page every 25 seconds only while visible;
pause on hidden tabs, errors or expired login, permit manual refresh, and show
last successful refresh/stale state. Do not replace a later page while reading.
Use accessible labels, keyboard navigation, semantic tables, an explicit empty
state, and a harness chart with equivalent text counts. Include metadata detail,
conflict links, login-denied/unavailable pages and logout. No merge, purge or
ingest controls appear in this UI.

## Release B retrieval contract

The viewer reads the published normalized generation, never starts workers and
never reads raw CAS through the browser. Add bounded event pages with generation
and position cursors. A generation change returns a clear conflict/reload result;
pending, failed, unknown or missing output is explicitly unavailable. Coordinate
file publication and reader snapshot acquisition so a response cannot mix
generations. Render text literally; retain opaque encrypted content as unavailable
text, not decrypted or searchable. Preserve event order, tool call/result IDs,
compactions, null usage and harness provenance.

Build a rebuildable SQLite FTS5 index of normalized `content_text`, keyed by
session UID, generation and event position. Index only the current successful
generation. Integrate its durable work with publication/restart/retry; a failed
index must not fail ingestion or claim search is current. Queries are bounded
literal-text searches, not SQL or user-supplied FTS syntax; apply session filters
and UTC recorded-time range. Show index lag; exclude stale/purged generations.
Rebuild from derived data, and integrate index deletion with purge and backup/
restore semantics. Avoid scanning every JSONL for each browser request.

Introduce `exporter` as an additional role; it implies viewer. Viewing alone
does not authorize bulk download. Export requires selected session UIDs, up to
100, and a format: `events`, `sharegpt`, or `trajectory`. An explicit selection
from search is supported; there is no implicit download of every search hit.
Use CSRF-protected POST and stream an attachment after preparing a bounded
snapshot. Limit each export to 64 MiB, two concurrent exports per process,
and two minutes; return an explicit error for exceeded limits before headers
where possible, and never report a truncated export as successful. Temporary
files live in the lake's private temp area and are cleaned on cancellation,
completion and restart. No persistent jobs or download URLs in this release.

Factor reusable projection logic out of the CLI without changing CLI behavior.
Server web config gains an explicit `export_projects` allow/deny policy using
existing project match semantics. Default deny gates every web format. This
extra server-side gate does not relax the uploading agent policy. Training
formats additionally preserve ruleset-v2 plaintext stripping, `raw_sha256`,
opaque encrypted content, and no-training-turn exclusions. Events remain
normalized data without an extra redaction pass, clearly disclosed in the UI.
Preflight the entire selection: forbidden, missing, stale/unavailable or
ineligible sessions produce a structured refusal before a download, without
silently skipping. Audit actor, selected UIDs, format, result and byte count;
never log transcript content. Raw blobs and Parquet downloads remain out of scope.

## Release C measurement contract

Record append-only catalog history for accepted head changes in the same
transaction as the head update: session UID, machine ID, harness, UTC receipt
time, prior/new head digest, new head logical size and relation. Idempotent
unchanged or stale reposts add no event. This measures accepted session updates,
not network ingress bytes or user activity time. Keep a rollout coverage timestamp;
do not backfill fake events from current session timestamps. Purge removes the
session's history; backup/restore includes it. No retention TTL is introduced.

Serve bounded UTC hourly/daily buckets for accepted updates and net logical head
size change, with harness filters and a maximum 90-day range. Negative changes
remain negative; label them as logical changes, not storage savings. Show the
coverage boundary, empty buckets, and purged-history limitations. Charts remain
read-only viewer features. Physical storage, online agents, token/cost analytics
and complete network throughput require separate future requirements.

## Verification and delivery

Use isolated temp lakes and synthetic sessions, never the operator's live data or
auth files. Test OIDC with a fake HTTPS IdP and ephemeral keys. Cover invalid
tokens/algorithms, state/nonce/replay, key rotation, authorization and expired
sessions, plus unchanged agent behavior. Exercise auth through the complete mux,
not only individual handlers. Test pagination against equal and mixed-precision
timestamps, concurrent ingestion and normalization, empty lakes and malicious text.

The release A gate seeds 20,000 metadata sessions, demonstrates bounded response
sizes and indexed pagination, and records representative timings without brittle
machine-specific timing assertions. Run `make ci` and `go test -race ./...` for
the implementation release; keep Makefile, justfile and both CI workflows aligned
if a new gate is added. Browser smoke covers login, denied access, filters,
refresh, metadata, conflicts, responsive layout and logout using synthetic data.
Document the exact smoke command and use a supported browser harness; no production
IdP is needed to close implementation tickets.

Update CLI help, architecture, protocol/browser API docs, deployment examples and
the VPS checklist. Document IdP registration, callback, group mapping, secret-file
permissions, TLS proxy settings and restart/session semantics. Live rollout needs
the actual origin, issuer, client registration, group mapping and secret-file
location supplied by deployment configuration. Do not guess them or provision
them as part of implementation. Existing go-live tickets retain their own gates;
dashboard completion is not proof that those checks passed.

## Ticket map

Ticket links track the current files; use `git ticket show ID` after future
status changes move a file. Release A and its eight children are done.

### Release A

Epic: [TKT-01M3F2FSTF28GNEDGQ0XBSZ44W — Web dashboard with OIDC and read-only lake visibility](../.tickets/done/TKT-01M3F2FSTF28GNEDGQ0XBSZ44W.md).

| Ticket | Work |
|---|---|
| [TKT-01M3F2K1EZ0CVDH1KPA127XYZ3](../.tickets/done/TKT-01M3F2K1EZ0CVDH1KPA127XYZ3.md) | Web: add explicit server configuration and exposure guards |
| [TKT-01M3F2K1J85QBJCBX9S462QG2V](../.tickets/done/TKT-01M3F2K1J85QBJCBX9S462QG2V.md) | OIDC: implement provider verification and group authorization |
| [TKT-01M3F2K1NMEEXDZSA3J140B075](../.tickets/done/TKT-01M3F2K1NMEEXDZSA3J140B075.md) | OIDC: add browser sessions, login routes and request guards |
| [TKT-01M3F2K1RTWH93KB9DSMWR21DQ](../.tickets/done/TKT-01M3F2K1RTWH93KB9DSMWR21DQ.md) | Catalog: track published normalization generations explicitly |
| [TKT-01M3F2K1ZDTPZRY5J6FKW451Q6](../.tickets/done/TKT-01M3F2K1ZDTPZRY5J6FKW451Q6.md) | Catalog: add bounded dashboard queries and stable pagination |
| [TKT-01M3F2K241CAKSX5QM5NGP24RW](../.tickets/done/TKT-01M3F2K241CAKSX5QM5NGP24RW.md) | Web API: expose authorized metadata reads through the lake mux |
| [TKT-01M3F2K27WTA90MB3K2M2AVZ6H](../.tickets/done/TKT-01M3F2K27WTA90MB3K2M2AVZ6H.md) | Web UI: build the lake overview and metadata browser |
| [TKT-01M3F2K2B7QW5SJZ9F8G65RBN5](../.tickets/done/TKT-01M3F2K2B7QW5SJZ9F8G65RBN5.md) | Web: validate OIDC dashboard and document hosted operation |


### Release B

Epic: [TKT-01M3F2PGA1EFCEPEBPT1JFR3JJ — Web retrieval: browse, search and export stored sessions](../.tickets/draft/TKT-01M3F2PGA1EFCEPEBPT1JFR3JJ.md). Depends on release A.

| Ticket | Work |
|---|---|
| [TKT-01M3F2PGDY06D7XE12NWQ9EZF4](../.tickets/draft/TKT-01M3F2PGDY06D7XE12NWQ9EZF4.md) | Web: browse normalized transcripts with generation-safe paging |
| [TKT-01M3F2PGHM6VHQBNE5XKDXS407](../.tickets/draft/TKT-01M3F2PGHM6VHQBNE5XKDXS407.md) | Search: index current normalized content with durable FTS5 work |
| [TKT-01M3F2PGMZKTFXSX521T07A4HA](../.tickets/draft/TKT-01M3F2PGMZKTFXSX521T07A4HA.md) | Web: add filtered transcript search and result navigation |
| [TKT-01M3F2PGRMS5NJZK90JCTAF0SP](../.tickets/draft/TKT-01M3F2PGRMS5NJZK90JCTAF0SP.md) | Export: add bounded authorized web downloads and shared projection |
| [TKT-01M3F2PGWAJRETEYE51GTX17DP](../.tickets/draft/TKT-01M3F2PGWAJRETEYE51GTX17DP.md) | Web retrieval: integrate downloads and validate the release |

### Release C

Epic: [TKT-01M3F2RKCZZNB6C1EGEG1FDCQH — Lake analytics: record and visualize accepted ingestion updates](../.tickets/draft/TKT-01M3F2RKCZZNB6C1EGEG1FDCQH.md). Depends on release A; it can be scheduled independently of release B because it uses catalog metadata only.

| Ticket | Work |
|---|---|
| [TKT-01M3F2RKGB79Y16RGTW3Z244QC](../.tickets/draft/TKT-01M3F2RKGB79Y16RGTW3Z244QC.md) | Catalog: record idempotent accepted head-update history |
| [TKT-01M3F2RKKRM1MP6GJ0P5BJ3JW7](../.tickets/draft/TKT-01M3F2RKKRM1MP6GJ0P5BJ3JW7.md) | Web analytics: add bounded ingestion charts and release validation |
