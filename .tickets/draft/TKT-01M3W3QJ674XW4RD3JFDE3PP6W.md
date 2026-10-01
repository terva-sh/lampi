---
schema: 3
id: TKT-01M3W3QJ674XW4RD3JFDE3PP6W
title: "Docs: one walkthrough from nothing to a fleet lake"
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/docs
  - area/ops
assignees: []
milestone: null
parent: TKT-01M3W3PZSDCB5FGKJS06MSZH65
origin: null
dependencies: []
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

An operator should go from nothing to a lake that a fleet of
machines syncs to by following one page. Today that means reading
docs/vps-bringup.md or docs/container.md, then docs/web-dashboard.md,
then docs/registration-and-lakes.md, and the best-practices ticket
adds another.

Write one walkthrough with two branches (systemd host, container
host) that ends with:

- the lake behind TLS, with the proxy body limit set for chunked
  uploads;
- the dashboard signed in through the operator's IdP, or through the
  Dex example when they have none;
- backups and metrics turned on, linking the backup and monitoring
  tickets rather than repeating them;
- a first machine enrolled with `install.sh` and a registration code,
  checked with `terva-lampi agent config` and a `sync`.

Each step says how to check it worked before the next one.

## Acceptance criteria

- [ ] One page covers a systemd host and a container host
- [ ] It ends with TLS, a signed-in dashboard, backups, metrics and one enrolled machine
- [ ] Each step says how to check it worked
