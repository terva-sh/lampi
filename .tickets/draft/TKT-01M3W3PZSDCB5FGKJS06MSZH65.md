---
schema: 3
id: TKT-01M3W3PZSDCB5FGKJS06MSZH65
title: "Easy deployment: a personal laptop lake and a lake for a fleet"
type: epic
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/ops
  - area/docs
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-01T16:10:43Z
updated_at: 2026-10-01T16:10:43Z
created_by:
  id: agent:claude-code/0f3154cf
  name: ""
updated_by:
  id: agent:claude-code/0f3154cf
  name: ""
extensions: {}
---

## Description

Running lampi correctly takes too much reading today. The pieces exist:
the agent and serve units in `deploy/systemd/`, the agent plist in
`deploy/launchd/`, the compose file, `register --install-service`,
`install.sh`, and the OIDC dashboard. But each one is documented on
its own, and the operator has to assemble them. This epic makes two
deployments easy and correct by default.

### Laptop lake

One person keeps their own sessions on their own machine. `serve` and
the agent run as user services on loopback, with no device token and
nothing leaving the machine. The dashboard works without an auth
system the person already runs. Today a laptop lake means running
`serve` in a terminal (docs/getting-started.md): there is no user unit
for `serve` on Linux, no launchd plist for it on macOS, and the
dashboard needs an OIDC provider.

### Fleet lake

A lake on a server, TLS in front, an IdP for the dashboard, and many
machines enrolled with registration codes. Most of this exists in
docs/vps-bringup.md, docs/container.md and the self-hosted lake epic
TKT-01M3MC023P4A5H7PTF662QSSM8 ("Self-hosted lake: container image,
registry, and operations"). What is missing is one path through it,
an IdP for an operator who has none, and a way to enroll many
machines without minting a code for each.

### Facts the children rely on

- The OIDC issuer and its discovered endpoints must be HTTPS:
  `webconfig.HTTPSURL` (internal/webconfig/config.go) has no loopback
  exception, and docs/web-dashboard.md says there is no bypass.
  `base_url` may be `http://` on loopback.
- A logged-in user gets no access unless a group claim maps to a role.
  Operator actions that add access need `auth_time` from a `max_age`
  re-login in the last 10 minutes.
- `serve` without `--token-file` accepts requests only on loopback.
  The agent's defaults are already `http://127.0.0.1:8787` and
  `~/.config/terva-lampi/token`.
- `register --install-service` writes the agent's systemd user unit or
  launchd agent (internal/cli/service.go). Nothing installs `serve`.
- Registration codes are one-time secrets with an expiry
  (internal/regcode/regcode.go).

### Related open work, not moved under this epic

- TKT-01M3MC0RXBFWR30R6Q7RY17MJD, Docs: best practices for
  self-hosting a lake at home.
- TKT-01M3MC0S3, Ops: Prometheus scrape config and alert rules.
- TKT-01M3MC0S0, Ops: scheduled and off-host lake backups.
- TKT-01M3MC0S6, Deploy: single-replica Kubernetes manifests.
- TKT-01M3FHHBC, Agent onboarding: registration codes, lake config,
  many lakes.

## Acceptance criteria

- [ ] A person goes from install.sh to a running laptop lake and agent with one command
- [ ] A laptop lake's owner signs in to the dashboard without running an auth system of their own
- [ ] An operator goes from nothing to a TLS-fronted fleet lake with enrolled machines by following one page
- [ ] Every unit and plist in deploy/ is checked in CI
