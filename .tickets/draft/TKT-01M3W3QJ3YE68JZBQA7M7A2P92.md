---
schema: 3
id: TKT-01M3W3QJ3YE68JZBQA7M7A2P92
title: "Deploy: Dex as a loopback IdP for a lake with no auth system"
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/auth
  - area/ops
assignees: []
milestone: null
parent: TKT-01M3W3PZSDCB5FGKJS06MSZH65
origin: null
dependencies:
  - TKT-01M3W3QJ0N1KYD6JK8MTM2C30X
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-01T16:11:02Z
updated_at: 2026-10-01T16:11:02Z
created_by:
  id: agent:claude-code/0f3154cf
  name: ""
updated_by:
  id: agent:claude-code/0f3154cf
  name: ""
extensions: {}
---

## Description

Ship a working Dex setup as the IdP for a lake with no auth system,
following the option chosen in the sign-in spike.

- A Dex config with one static client for the lampi dashboard
  (callback `<base_url>/auth/oidc/callback`, S256 PKCE) and one local
  user, with the groups the role map needs. Secrets are files the
  operator creates, never values in the example.
- Run it three ways: a systemd user unit, a launchd agent plist, and
  an optional service in `deploy/compose/compose.yaml` for a fleet
  operator who has no IdP either.
- A `web-config.json` example wired to that Dex, and the serve units
  passing `--web-config`.
- A docs section in docs/web-dashboard.md: start Dex, sign in, mint a
  registration code (which needs `auth_time`).
- An e2e or smoke test that signs in through the example Dex config,
  so the example breaks a build instead of a user.

## Acceptance criteria

- [ ] A Dex config, systemd user unit, launchd plist and compose service ship in deploy/
- [ ] A web-config example signs in through that Dex and can mint a registration code
- [ ] A test signs in through the example config
