---
schema: 3
id: TKT-01M3F2K1NMEEXDZSA3J140B075
title: "OIDC: add browser sessions, login routes and request guards"
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area/auth
  - area/server
assignees: []
milestone: null
parent: TKT-01M3F2FSTF28GNEDGQ0XBSZ44W
origin: null
dependencies:
  - TKT-01M3F2K1J85QBJCBX9S462QG2V
blocks_on: none
references:
  - ref: plan:web-ui
    path: docs/web-ui-plan.md
claim: null
archive: null
created_at: 2026-09-26T14:40:58Z
updated_at: 2026-09-26T14:40:58Z
created_by:
  id: agent:codex/web-ui-planning
  name: ""
updated_by:
  id: agent:codex/web-ui-planning
  name: ""
extensions: {}
---

## Description

### Scope and rationale

Add /auth/oidc/start, /auth/oidc/callback and CSRF-protected POST /auth/oidc/logout. Bind each ten-minute attempt to its browser, consume it once and cap outstanding attempts. Sessions are in memory with one-hour idle and twelve-hour absolute expiry; polling cannot extend the latter. Use production __Host- cookies and separate loopback-development cookies. No provider token persistence. Page/API guards must remain distinct from device auth.

### Contract and references

Follow docs/web-ui-plan.md, including pinned sibling sources and release A defaults. Use isolated synthetic data. Newly filed work stays draft pending promotion.

## Acceptance criteria

- [ ] Login, callback and logout implement the documented cookies, attempt binding, local return paths and role checks.
- [ ] Replay, state mismatch, browser swap, expired attempts and cross-origin logout fail; GET logout cannot revoke.
- [ ] Idle and hard expiry, restart and logout invalidate sessions, including continued polling past hard expiry.
- [ ] Browser cookies never grant device API access and device bearer tokens never grant browser API access.
- [ ] Responses/logs contain no tokens, cookies, client secrets, codes, state or provider response bodies.

## Definition of done

- [ ] Focused tests pass and behavior/contracts are documented; record validation and rationale in the ticket.

## Implementation plan

Implement bounded session/attempt stores with injectable clocks and sweeping. Validate local return paths including encoded redirects. Set Secure/HttpOnly/SameSite=Lax cookies according to configured public origin. Protect logout against cross-origin requests and revoke the server record. Guard page requests with login redirect and JSON API requests with 401; mapped-role failures are 403. Test through mux composition, including swapped-browser callbacks, replay, expired attempts, invalid cookies, session caps, restart invalidation and hard expiry under repeated polling.
