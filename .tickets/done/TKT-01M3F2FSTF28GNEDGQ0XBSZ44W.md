---
schema: 3
id: TKT-01M3F2FSTF28GNEDGQ0XBSZ44W
title: Web dashboard with OIDC and read-only lake visibility
type: epic
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area/server
  - area/auth
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: children
references:
  - ref: plan:web-ui
    path: docs/web-ui-plan.md
claim: null
archive: null
created_at: 2026-09-26T14:39:12Z
updated_at: 2026-09-26T15:28:37Z
created_by:
  id: agent:codex/web-ui-planning
  name: ""
updated_by:
  id: agent:codex/web-ui-release-a
  name: ""
extensions: {}
---

## Description

### Outcome

Release A of docs/web-ui-plan.md: one small read-only web dashboard in the existing binary, with application-managed OIDC from the first release. The owner approved this direction after review of Terva, Git Ticket Canvas and Ketju. This epic waits on its direct children.

### Decisions and rationale

Use Go templates and embedded assets with a separate browser read API. A frontend framework and separate service add unnecessary build and deployment work for the first view. Follow sibling Go OIDC libraries and group mapping rather than introducing proxy identity. Start with bounded in-memory sessions; hard expiry prevents dashboard polling from retaining old group claims indefinitely. Require device tokens whenever web is enabled, even with a loopback backend. Do not infer normalization success from an empty error.

### Execution

The linked design is the implementation contract, including route names, configuration, pagination, status semantics and test defaults. Children provide the dependency order and own their focused checks. Work against isolated lakes and a fake HTTPS IdP. No production endpoint, credential, IdP provisioning or deployment is needed to complete this epic. Existing deployment-validation tickets remain independent gates. No transcript reading, export, administration or throughput charts in this release. Newly filed tickets remain draft pending owner promotion.

## Acceptance criteria

- [x] An authorized viewer can sign in and inspect overview, sessions, metadata and conflicts without a device token.
- [x] OIDC, read APIs, normalization status and browser behavior meet release A in docs/web-ui-plan.md.
- [x] Agent protocol regressions, fake-IdP integration, 20k-session validation and operator documentation are complete.

## Implementation plan

Confirm all eight release A children are done, verify implementation and validation evidence, then close the dashboard epic; retrieval and analytics remain draft.

## Summary

Release A is implemented and validated in the existing static Go binary. Explicit --web-config enables application OIDC with group-mapped viewer access, secure bounded in-memory browser sessions and read-only metadata pages/API. The UI provides overview/harness/normalization counts, filtered cursor-paginated sessions, artifact history, provenance and conflicts, with accessible no-JS forms and visibility-aware refresh. Device ingestion auth and protocol behavior remain separate. Catalog migrations track successful generation/head publication and indexed chronological head timestamps; old rows remain unknown rather than falsely ready. All eight children are done. Full make ci, go test -race ./..., fake-HTTPS-IdP browser smoke, 20k catalog checks and concurrent device/browser ingestion tests passed; focused cursor parsing race/vet checks passed after final strictness review. docs/web-dashboard.md and docs/web-api.md document operation and contracts; e2e/README.md documents reproducible synthetic validation. No deployment or IdP provisioning was performed. Retrieval and analytics epics remain draft.
