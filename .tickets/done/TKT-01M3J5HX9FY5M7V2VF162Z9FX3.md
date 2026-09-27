---
schema: 3
id: TKT-01M3J5HX9FY5M7V2VF162Z9FX3
title: "Dashboard: mint, list and cancel registration codes"
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
  - area/auth
  - policy
assignees: []
milestone: null
parent: null
origin: null
dependencies:
  - TKT-01M3J5HX8GGBVSVVHX82W60VJE
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-27T19:30:30Z
updated_at: 2026-09-27T22:22:37Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/e4a47e8c
  name: ""
extensions: {}
---

## Description

Let an operator mint, list and cancel registration codes in the web dashboard, and copy a one-liner that installs terva-lampi and registers the machine. Today this is only `serve register --name/--list/--revoke` on the lake host.

This is the dashboard's first write action. Until now the dashboard has been read-only, and the web config accepts only the `viewer` role.

### Decisions (owner, 2026-09-27)

- **A new `operator` role**, mapped to its own IdP group in the web config. Viewers never see the page or its routes; the routes answer 404 to them, not 403. This is the first write role, so the policy doc records it.
- **A fresh sign-in to mint.** When the OIDC `auth_time` is older than 10 minutes, minting sends the user back through the IdP with `max_age`. Listing and cancelling need only the operator session.
- **Codes only.** Device listing, revocation and profile assignment are a separate follow-up.
- **One copy installs and registers.** This depends on the installer ticket (TERVA_LAMPI_CODE and --fingerprint).

### Approach

- **Mint.** The form takes a name, a profile (from the loaded `profiles.json`, default `default`) and an expiry. The expiry is 1 hour for the one-liner, with longer choices up to the existing 30-day cap. It goes through the same code path as `serve register`: public URL set, key-list self-check through that URL, `CreateRegistration`, `regcode.Encode`.
  - The response is a POST result with `Cache-Control: no-store`. It shows the code exactly once. It is never put in a URL, a log line or the session, and a reload does not show it again, since only its hash is stored.
  - It offers two copy buttons:
    1. The one-liner: ` curl -fsSL https://raw.githubusercontent.com/terva-sh/lampi/<lake release tag>/install.sh | TERVA_LAMPI_CODE='<code>' sh -s -- --register --fingerprint <SHA256:...>`, with a leading space. The tag comes from the lake's own build info, so the machine installs the version the lake runs. A lake built without a tag falls back to the latest release with a note.
    2. The code alone, for `terva-lampi register` on a machine that already has the binary.
  - It shows the lake id and fingerprint for comparison, and says the code works once and when it expires.
- **List.** Codes grouped by state (pending, used, expired, revoked) with name, profile, created, expiry, who minted it, and for used codes the device id and name. Listing records expiries (`RecordExpiries`), as the CLI does.
- **Cancel.** A CSRF-checked POST that revokes a pending code through `RevokeRegistration`. A used code has no cancel button. It links to the device instead, until the devices follow-up exists.
- **Attribution.** Add `created_by` and `revoked_by` to `registrations` (catalog schema 7, additive). They hold the OIDC subject and display name for dashboard actions and `cli` for `serve register`. `audit.jsonl` entries carry the same actor.
- **Web API.** Operator-only JSON routes under `/api/web/v1/registrations`, and pages under `/admin/registrations`. Both use the existing CSRF and Origin checks, and rate limits apply to minting.
- Docs: `docs/web-dashboard.md`, `docs/web-api.md`, `docs/policy.md`.

### Risk

With this change a stolen operator browser session can add a device to the lake, where before any session could only read. The separate role, the 10-minute sign-in check for minting, the one-hour default expiry and the audit trail are the mitigations. A device token can upload but cannot read the catalog.

## Acceptance criteria

- [x] An operator role mapped to its own group; viewers get 404 on every registration route
- [x] Minting requires an IdP sign-in within 10 minutes
- [x] A minted code is shown once with no-store and copy buttons for the one-liner and the code
- [x] The one-liner pins install.sh to the lake's release tag and carries the fingerprint
- [x] Codes list by state with who minted them; a pending code can be cancelled
- [x] created_by and revoked_by recorded in the catalog and audit.jsonl
- [x] Docs updated: web dashboard, web API, policy

## Implementation plan

Four PRs, to stay under the review size limit (revised from three: the mint path moved into its own package first).

1. Auth (#36, merged): an operator role in role_map (it implies viewer). webauth.OperatorOnly answers 404 to non-operators. Fresh sign-in: /auth/oidc/start?fresh=1 sends max_age=600, and the callback requires auth_time within 10 minutes (1 minute of skew). Identity carries Operator and AuthTime.
2. Catalog schema 7 (#37, merged): registrations.created_by and revoked_by. serve register records 'cli'.
3. internal/registrar: Mint, Revoke, List and AuditExpiries, moved out of cli/registerserve.go so serve register and the dashboard share one path (public URL self-check, CreateRegistration, regcode.Encode, audit before the code is returned). Actor{Catalog, Audit} names who acted in the catalog and in audit.jsonl.
4. Dashboard: /admin/registrations (list by state, mint form, cancel) and operator-only /api/web/v1/registrations, calling registrar with Actor web:<subject>. Mint requires Fresh and otherwise redirects to FreshLoginURL. The minted code shows once with no-store, with copy buttons for the one-liner (install.sh pinned to the lake's release tag, TERVA_LAMPI_CODE, --fingerprint) and for the code alone. Docs.

## Notes

**agent:claude-code/e4a47e8c** at 2026-09-27T21:53:56Z

PR 1 (auth) evidence. Tests: a viewer gets 404 on the operator page and API routes and an operator gets 200. A plain login sends no max_age and reads as stale when single sign-on reports an hour-old auth_time. A fresh login sends max_age=600 and reads as fresh. A fresh login is refused (403, no session) when the provider returns an old or missing auth_time. freshAt covers its window edges and skew. docs/web-dashboard.md, docs/policy.md (owner decision recorded) and the web-config example map an operator group.

**agent:claude-code/e4a47e8c** at 2026-09-27T22:00:25Z

PR 2 (catalog/registration-actors): schema 7 adds registrations.created_by (NOT NULL DEFAULT '', empty for older codes) and revoked_by. CreateRegistration and RevokeRegistration take a 'by' actor; serve register passes catalog.ActorCLI ('cli') and --list prints created_by= and revoked_by=. The audit Actor for the CLI stays 'serve register'. The dashboard (PR 3) will pass its operator identity. A second revoke keeps the first revoker. Chose a parameter rather than a context value so every caller has to name its actor.

**agent:claude-code/e4a47e8c** at 2026-09-27T22:09:01Z

PR 4a (web/registrations-api): operator-only JSON routes. The dashboard actor is 'web:SUBJECT (DISPLAY)', control characters dropped and capped at 256 bytes, the same string in created_by/revoked_by and the audit actor. Writes carry the session CSRF in X-Lampi-CSRF, checked with webauth.Browser.CheckWrite (the same token, Origin and Sec-Fetch-Site checks as sign-out). Mint needs Fresh (403 fresh_login_required with a login URL); list and revoke do not. Revoke takes only reg_ ids, because a name could match a code the operator did not see. Minting is rate limited in-process (burst 5, one per 12s), since each mint fetches the key list through the public URL and syncs an audit line. The install line pins install.sh and --version to the lake's build tag only when it is a plain vX.Y.Z; pseudo-versions, +dirty and prereleases fall back to main and the latest release, with install_pinned false. serve register --list quotes an actor with spaces. The pages come in PR 4b.

**agent:claude-code/e4a47e8c** at 2026-09-27T22:22:36Z

PR 4b (web/registrations-pages): /admin/registrations lists codes by state with who minted and cancelled each, the mint form (replaced by 'Sign in again to mint' when the sign-in is older than 10 minutes; a stale POST redirects to the fresh login), the minted code shown once on the POST result with copy buttons for the install line and the code alone, and Cancel as a CSRF form POST. The refresh bar is left off operator pages so a manual refresh cannot replace a code the operator has not copied yet. The mint form offers fixed expiries (1h default, 1d, 3d, 7d, 30d); anything else is refused. Checked in headless Chromium against the smoketest (-operator): the fresh sign-in round trip, the mint, the clipboard getting the line with its leading space, the refusal, and a cancel. AC6 note: serve register records 'cli' in the catalog, and its audit actor stays 'serve register'/'serve register --revoke' as before; the dashboard writes the same web:SUBJECT (DISPLAY) string to both.

## Summary

Operators can mint, list and cancel registration codes in the dashboard. PRs: #36 added the operator role (a viewer gets 404) and the fresh sign-in (max_age, auth_time within 10 minutes). #37 added catalog schema 7 (registrations.created_by and revoked_by). #38 moved the mint path into internal/registrar, shared with serve register. #39 added the operator JSON API under /api/web/v1/registrations (CSRF header, fresh sign-in to mint, rate limit, install line pinned to the lake's release tag). This PR adds the /admin/registrations pages. Codes are shown once with copy buttons for the one-liner and for the code alone. The devices UI is still the follow-up, TKT-01M3J5HXA.
