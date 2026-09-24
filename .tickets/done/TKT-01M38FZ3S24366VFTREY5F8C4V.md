---
schema: 3
id: TKT-01M38FZ3S24366VFTREY5F8C4V
title: Document VPS lake bring-up runbook
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/docs
  - area/ops
  - phase/0-policy
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-24T01:20:04Z
updated_at: 2026-09-24T01:23:56Z
created_by:
  id: agent:cursor/1035
  name: Cursor cloud agent
updated_by:
  id: agent:cursor/1035
  name: Cursor cloud agent
extensions: {}
---

## Description

Phase 0 already places the lake on a small VPS. This ticket is the operator checklist and the example serve unit for that decision: encrypted disk, loopback serve, TLS in front, device tokens, and LAMPI_SERVER on the agents.

The checklist does not provision a host. Placeholders stay copy-and-edit. The production hostname and tokens stay off this tree.

## Acceptance criteria

- [x] Ordered checklist covers disk, binary, data dir, device token, loopback serve, TLS, healthz, and LAMPI_SERVER
- [x] Serve systemd unit and env example bind loopback and stay uninstalled examples
- [x] Placeholders only: no production hostname, token, or live provisioner
- [x] Soft-link draft TKT-01M3558E21PZDJEYAAR4WH9B85 stays draft

## Definition of done

- [x] Runbook linked from deploy/README.md and docs/policy.md
- [x] git ticket check --strict passes

## Implementation plan

Add docs/vps-bringup.md as the ordered operator checklist from docs/policy.md: encrypted volume or LUKS, install the binary, --data layout on that volume, device token copy at mode 0600, loopback serve, TLS in front, /healthz, and LAMPI_SERVER on the agents.

Add deploy/systemd/terva-lampi-serve.service and serve.env.example. The unit is a system service, unlike the agent user unit, because the lake should start at boot. It binds 127.0.0.1:8787. systemd substitutes LAMPI_SERVE_ADDR, LAMPI_SERVE_DATA, and LAMPI_SERVE_TOKEN_FILE into --addr, --data, and --token-file. serve does not read LAMPI_SERVER or LAMPI_TOKEN_FILE; those stay agent settings.

Link the runbook from deploy/README.md, docs/policy.md, and the README packaging section. Examples use lake.example and local paths. No hostname from a real host, no token, no SSH provisioner.

Leave TKT-01M3558E21PZDJEYAAR4WH9B85 draft.

## Summary

docs/vps-bringup.md is the Phase 0 operator checklist: encrypted volume or LUKS, the binary, --data on that volume, a mode-0600 device token that serve rewrites to sha256:<hex>, loopback serve, TLS in front, /healthz, and LAMPI_SERVER on the agents. deploy/systemd/terva-lampi-serve.service and serve.env.example are the system unit. systemd substitutes LAMPI_SERVE_* into --addr, --data, and --token-file. serve does not read LAMPI_SERVER or LAMPI_TOKEN_FILE. Examples use lake.example and local paths. TKT-01M3558E21PZDJEYAAR4WH9B85 stays draft. go test ./..., go vet ./..., and git ticket check --strict passed.
