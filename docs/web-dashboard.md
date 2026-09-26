# Serving the OIDC dashboard

The first browser release shows lake totals, a harness breakdown, normalization
status, recent/filtered sessions, artifact metadata, provenance and divergent
copies. It is read-only. Transcript browsing, search, downloads and ingestion
charts remain future releases in [web-ui-plan.md](web-ui-plan.md).

## Register the application

Use your identity provider's OIDC application registration. Terva's sibling
implementation was built against Authentik; Lampi uses discovery and is not
vendor-specific. Operator-supplied values are the public lake origin, issuer URL,
client ID, client secret file if using a confidential client, and viewer group.
The repository does not name or provision a live deployment.

1. Register Authorization Code with S256 PKCE. A confidential client uses a
   secret; a public client may omit `client_secret_file` if the provider permits it.
2. Register the exact callback `https://lake.example/auth/oidc/callback`, replacing
   the origin with the deployed origin. No wildcard redirect is needed.
3. Supply `openid`, `profile`, `email`, and the scope that supplies group membership
   in the **ID token**. The default extra scopes are profile/email/groups; customize
   `scopes` when your provider uses different scopes. `openid` is always included.
4. Map the actual group claim and exact group names. A successful IdP login grants
   no access unless a configured group maps to `viewer`. The role reads metadata
   for the whole lake; this release has no per-project viewer isolation.

Use HTTPS for the issuer and discovered authorization/token/JWKS endpoints. A
private CA belongs in the service's system trust store. There is no TLS or issuer
verification bypass. The callback is derived from configuration, never request
Host or forwarded headers. Avoid putting browser sign-in middleware in front of
`/v1`: agents must continue to use their device tokens.

## Configure the service

Start from [deploy/web-config.json.example](../deploy/web-config.json.example).
All values in that example are placeholders. `base_url` is an origin only, with
no trailing slash, path, query or fragment. The server config is independent of
an agent's `config.json`; serve never loads the agent's allowlist or credentials.
Unknown JSON fields, unknown roles and an empty role map fail startup.

Store a confidential client secret in an absolute-path, regular file readable
only by its owner (0600 on Unix), owned by the service user. It must contain only
the secret, optionally followed by a newline, and be at most 4096 bytes. Do not
put it in JSON, process arguments, shell history or source control. The example
uses `/etc/terva-lampi/oidc-client-secret`; the service needs read access, not
write access. A public client omits that field entirely. Protect the web config
as operator configuration too; it grants access through its group map.

With the paths from the existing deployment examples:

```sh
terva-lampi serve --addr 127.0.0.1:8787 \
  --data /var/lib/terva-lampi \
  --token-file /var/lib/terva-lampi/tokens \
  --web-config /etc/terva-lampi/web.json
```

The example systemd unit accepts `LAMPI_SERVE_WEB_CONFIG` in
`/etc/terva-lampi/serve.env`. Set it to the absolute web config path and restart
the service. It defaults to empty, leaving web disabled. This is systemd argument
substitution, not a new environment variable that the Go process reads itself.
The existing sandbox permits reading `/etc/terva-lampi`; `ProtectHome` still
prevents reading files placed in a user's home.

**Web enabled requires a nonempty device-token set even on loopback.** Otherwise
publishing a loopback backend through a proxy could open `/v1` ingestion. A
browser cookie cannot authorize `/v1`, and a device token cannot authorize the
browser API. The existing public `/healthz` remains a data-free process probe.
Omitting `--web-config`, or setting it empty, disables the page/auth/browser API
routes without changing existing agent behavior.

Web configuration changes, including group mappings and client secrets, require
restart. SIGHUP reloads device tokens only. Invalid local web configuration fails
before listening. Discovery is lazy: an unavailable IdP returns a sign-in error,
while ingestion and existing browser sessions continue. A later login retries.
Provider requests have ten-second budgets and at most eight concurrent operations.

## TLS proxy and sessions

Keep the Go listener on loopback, behind the HTTPS proxy in
[vps-bringup.md](vps-bringup.md). Proxy all paths to that listener, preserve cookies,
and do not cache authenticated responses or strip their `no-store` header. Keep
the existing upload body limits, streaming and timeouts. No CORS configuration is
needed. Do not log auth callback query strings, Authorization or Cookie headers at
the proxy. For nginx, use `$uri` rather than `$request` or `$request_uri` in an
access-log format; those latter variables contain the callback authorization code.
The application's access log already excludes query strings and headers carrying
credentials. Auth failures add fixed reason messages, not provider error bodies.

Production cookies are HttpOnly, Secure, SameSite=Lax, host-only and use a
`__Host-lampi_` prefix. A session expires after one hour idle or twelve hours total.
Polling counts as activity, but never extends the absolute expiry. Group membership
is captured at login; IdP membership changes take effect at next login or hard
expiry. Restart revokes all sessions. Sign out revokes the local session using
CSRF-protected POST; it does not sign the user out of their identity provider.
The provider may sign them in again without another password prompt.

A plain HTTP browser origin is accepted only on loopback for development, using
separate unprefixed development cookies. This exception does not permit a plaintext
issuer. Tests use a fake HTTPS IdP and an injected trust pool, not a production
configuration bypass.

## What the dashboard means

- Artifact versions count catalog rows, not unique blobs or disk usage.
- Contributing machines have uploaded data; the count is not online status.
- Last head update is not a history of every upload or a throughput measurement.
- Pending includes queued, running and retrying normalization. Failed means a
  recorded terminal failure. Ready requires publication of the current generation
  and head. Unknown includes legacy rows not verified by a new publication.
- Metadata lists use bounded live cursor pages. Ingestion may move a session to
  an earlier page; refresh to start over. Large labels/paths are display previews.
  The [browser API contract](web-api.md) describes exact limits.

Overview and the first session page refresh every 25 seconds while visible.
Other pages stay still. Refresh pauses on failures, expired login, hidden tabs or
keyboard focus inside the updated table. Manual refresh retries. Forms, navigation
and tables also work without JavaScript. Raw transcripts, export and administration
are absent from this release.

## Validation, backup and rollout

The catalog upgrade adds published-generation/head markers and indexed numeric
head-update timestamps. Take the normal lake backup before upgrading a deployed
lake. An older binary refuses the newer schema; rollback requires a compatible
binary or an appropriate pre-upgrade backup, not editing `user_version`.

`serve backup` retains its existing scope: catalog, blobs and device-token copy.
Web configuration and the external OIDC secret file need your separate protected
configuration backup. In-memory browser sessions never enter a backup. Restore
and reconfigure the service, then sign in again.

Run local gates with Go 1.27 (`mise exec --` where Go is managed by mise):

```sh
make ci
go test -race ./...
go test ./internal/catalog -run TestDashboard20K -count=1 -v
```

[e2e/README.md](../e2e/README.md#browser-dashboard-smoke) documents the automated
Chromium smoke. It covers login, denied access, pagination, filters, metadata,
conflicts, refresh failures/recovery, mobile/keyboard use, no-JS, empty lake and
logout against temporary synthetic lakes. No live credentials are needed.

Before a real rollout, provide the actual origin/issuer/client/group/secret path,
verify the IdP callback and claim mapping, and apply the existing VPS go-live gates.
The dashboard release does not certify the separate restore, canary-secret, crash
or throttled-upload tickets. Building or testing this feature does not deploy it.
