---
schema: 3
id: TKT-01M3F2K1EZ0CVDH1KPA127XYZ3
title: "Web: add explicit server configuration and exposure guards"
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area/server
  - area/auth
assignees: []
milestone: null
parent: TKT-01M3F2FSTF28GNEDGQ0XBSZ44W
origin: null
dependencies: []
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

Add serve --web-config PATH independently of agent config.json. Implement the exact server configuration and route-disabled semantics in docs/web-ui-plan.md. Read OIDC secrets from client_secret_file only, with no CLI inline secret. Validate local configuration before listening. A web-enabled server must have nonempty device tokens even when its backend binds loopback, because the TLS proxy publishes /v1 too. Existing serve without web config keeps its behavior. Keep SIGHUP limited to device-token reload; changes to web config require restart.

### Contract and references

Follow docs/web-ui-plan.md, including pinned sibling sources and release A defaults. Use isolated synthetic data. Newly filed work stays draft pending promotion.

## Acceptance criteria

- [ ] serve --web-config is explicit; absent config leaves all new web routes disabled and existing CLI behavior intact.
- [ ] Invalid configuration and web-enabled empty device-token sets fail before listening, including loopback behind a proxy.
- [ ] issuer/client_id/scopes/groups_claim/role_map and secret-file settings follow the design; secret values never appear in errors, help or examples.
- [ ] Focused config and CLI tests pass; configured callbacks cannot be changed by Host or forwarded headers.

## Definition of done

- [ ] Focused tests pass and behavior/contracts are documented; record validation and rationale in the ticket.

## Implementation plan

Read internal/cli/serve.go, internal/config/config.go and serve tests. Add strict web configuration types in a separate server-owned file/package, validation, safe secret loading, help text and placeholder-only example. Introduce a composition option for a later web handler without exposing unfinished routes. Test web off, valid public/loopback origins, rejected origins/unknown keys/roles/empty mappings and empty device-token sets. Do not read the live agent configuration.
