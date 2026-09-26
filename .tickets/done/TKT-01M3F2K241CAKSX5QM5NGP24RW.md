---
schema: 3
id: TKT-01M3F2K241CAKSX5QM5NGP24RW
title: "Web API: expose authorized metadata reads through the lake mux"
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
  - area/protocol
assignees: []
milestone: null
parent: TKT-01M3F2FSTF28GNEDGQ0XBSZ44W
origin: null
dependencies:
  - TKT-01M3F2K1NMEEXDZSA3J140B075
  - TKT-01M3F2K1ZDTPZRY5J6FKW451Q6
blocks_on: none
references:
  - ref: plan:web-ui
    path: docs/web-ui-plan.md
claim: null
archive: null
created_at: 2026-09-26T14:40:59Z
updated_at: 2026-09-26T15:11:11Z
created_by:
  id: agent:codex/web-ui-planning
  name: ""
updated_by:
  id: agent:codex/web-ui-release-a
  name: ""
extensions: {}
---

## Description

### Scope and rationale

Expose only the release A browser routes in docs/web-ui-plan.md. Reuse the dashboard catalog query layer and viewer guard without accepting a device token as browser identity. Keep /v1 protocol contracts and /healthz unchanged. Avoid raw manifest/extra fields, transcript content and filesystem paths supplied as selectors.

### Contract and references

Follow docs/web-ui-plan.md, including pinned sibling sources and release A defaults. Use isolated synthetic data. Newly filed work stays draft pending promotion.

## Acceptance criteria

- [x] Authorized viewers can call all documented release A browser endpoints; unauthenticated, unmapped and device-token-only requests receive the specified refusals.
- [x] Metadata DTOs exclude transcript/raw manifest bodies and expose bounded lists with validated cursor/filter contracts.
- [x] /healthz and every existing /v1 contract remain unchanged and browser cookies cannot authorize ingest.
- [x] No-store and relevant security headers apply; errors and logs do not disclose credential material.
- [x] In-flight browser reads participate in graceful shutdown without double logging or starting normalization work.

## Definition of done

- [x] Focused tests pass and behavior/contracts are documented; record validation and rationale in the ticket.

## Implementation plan

Compose browser routes with existing access/deadline/active-request/shutdown middleware exactly once. Implement overview, list, detail and conflicts responses with items/next_cursor/as_of, documented filters and bounded child collections. Guard before lookup; use JSON 400/401/403/404/5xx with safe failure categories. Add no-store/security headers and ensure request logging never includes callback query credentials. Test against the complete handler, covering malicious metadata, cancellation, concurrent ingest and the auth separation matrix; update browser API documentation.

## Summary

Added viewer-only /api/web/v1 overview, sessions, detail and paginated artifact/provenance/conflict routes. New web handler is composed inside the lake access/deadline/shutdown wrapper, with no duplicate logging. CLI now attaches it only after explicit validated web configuration. Read APIs guard before lookup, enforce filters/limits, use five-second cancellable catalog budgets and return fixed JSON errors with no-store/security headers. Full-mux fake-IdP tests exercise all endpoints, malformed inputs, metadata-only DTOs, cancellation, disabled web, unchanged health/device access and absence of callback/session credentials in logs. mise exec -- go test -race ./internal/web ./internal/cli -run 'TestReadAPI|TestWebOff|TestWebRequires' passes. docs/web-api.md records exact child collection routes and preview limits.
