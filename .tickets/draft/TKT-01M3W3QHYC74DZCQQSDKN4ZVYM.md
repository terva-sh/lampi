---
schema: 3
id: TKT-01M3W3QHYC74DZCQQSDKN4ZVYM
title: "Laptop lake: one command installs serve and the agent as services"
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/ops
  - area/agent
  - area/server
assignees: []
milestone: null
parent: TKT-01M3W3PZSDCB5FGKJS06MSZH65
origin: null
dependencies:
  - TKT-01M3W3QHV2GF2MT757H4DX1C8S
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

A person who wants to keep their own sessions should get a running
lake and agent from one command after `install.sh`, and not need to
read deploy/README.md first.

Something like `terva-lampi local install` (name to decide) that:

- writes and enables the `serve` user service and the agent user
  service, using the units from the examples ticket and the same code
  path as `register --install-service` (internal/cli/service.go);
- leaves an existing unit alone, as `--install-service` does;
- waits for `/healthz` and prints where the data lives, that
  `identity.json` needs backing up, and the next step (allow a
  project);
- has an `uninstall` that stops and removes the units and keeps the
  data.

Refuse when the machine already runs a lake or an agent for another
lake, and say which, rather than adding a second one: AGENTS.md
explains what a duplicate registration costs.

## Acceptance criteria

- [ ] One command installs, enables and health-checks serve and the agent on Linux and macOS
- [ ] It leaves existing units alone and refuses on a machine that already syncs to another lake
- [ ] An uninstall removes the services and keeps the data
