---
schema: 3
id: TKT-01M3J5HX9FY5M7V2VF162Z9FX3
title: "Dashboard: mint, list and cancel registration codes"
type: task
status: ready
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
updated_at: 2026-09-27T20:52:53Z
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

- [ ] An operator role mapped to its own group; viewers get 404 on every registration route
- [ ] Minting requires an IdP sign-in within 10 minutes
- [ ] A minted code is shown once with no-store and copy buttons for the one-liner and the code
- [ ] The one-liner pins install.sh to the lake's release tag and carries the fingerprint
- [ ] Codes list by state with who minted them; a pending code can be cancelled
- [ ] created_by and revoked_by recorded in the catalog and audit.jsonl
- [ ] Docs updated: web dashboard, web API, policy
