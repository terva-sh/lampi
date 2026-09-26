---
schema: 3
id: TKT-01M3F2K1J85QBJCBX9S462QG2V
title: "OIDC: implement provider verification and group authorization"
type: task
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area/auth
assignees: []
milestone: null
parent: TKT-01M3F2FSTF28GNEDGQ0XBSZ44W
origin: null
dependencies:
  - TKT-01M3F2K1EZ0CVDH1KPA127XYZ3
blocks_on: none
references:
  - ref: plan:web-ui
    path: docs/web-ui-plan.md
claim: null
archive: null
created_at: 2026-09-26T14:40:58Z
updated_at: 2026-09-26T14:58:20Z
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

Implement a provider-neutral relying party following the pinned Terva and Canvas references in docs/web-ui-plan.md. Use coreos/go-oidc/v3 and x/oauth2 rather than custom JWT verification. HTTPS discovery and all discovered endpoints are mandatory, with no issuer-check bypass. Use lazy bounded discovery that retries after failure and does not take ingestion down. Map exact configured groups to viewer; identity is issuer plus subject, not email.

### Contract and references

Follow docs/web-ui-plan.md, including pinned sibling sources and release A defaults. Use isolated synthetic data. Newly filed work stays draft pending promotion.

## Acceptance criteria

- [x] Fake HTTPS IdP completes a valid flow and exact mapped groups yield viewer; unmapped or malformed groups grant no access.
- [x] Wrong issuer/audience/signature/nonce, expired or subjectless tokens and symmetric/unsigned algorithms are refused.
- [x] Issuer and discovered endpoints must be HTTPS; key rotation succeeds without allowing attacker-chosen issuers.
- [x] IdP outage is retried with bounded calls and cannot disable ingestion or expose credentials in logs.

## Definition of done

- [x] Focused tests pass and behavior/contracts are documented; record validation and rationale in the ticket.

## Implementation plan

Add internal/webauth provider code separated from existing device auth. Implement authorization URLs and code exchange with S256 PKCE, signature/issuer/audience/expiry/subject/nonce validation and pinned asymmetric algorithms. Inject HTTP client and clock where needed for a fake HTTPS IdP. Read sibling licenses before copying code and preserve attribution if required. Add tests for key rotation, bad algorithms/keys/issuer/audience/expiry/nonce, missing subject and malformed/unmapped group claims. Bound provider requests and redact transport/provider error details before logging.

## Summary

Implemented local relying-party code using coreos/go-oidc v3 and x/oauth2, with HTTPS-only bounded transport, fixed asymmetric algorithms, issuer/audience/expiry/subject/nonce verification and exact group-to-viewer mapping. Independent implementation follows sibling patterns without copying source. Fake HTTPS IdP tests cover valid exchange, PKCE parameters, rotation, bad signature/issuer/audience/expiry/nonce/subject, none/HMAC algorithms, malformed/unmapped groups and recovery after outage/insecure discovered endpoints. mise exec -- go test ./internal/webauth passes. Provider failures are fixed categories; response bodies and tokens are never propagated. Eight provider operations maximum and ten-second network budgets bound resource use.
