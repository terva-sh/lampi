---
schema: 3
id: TKT-01M3KA70421QZ1PX3H2G5EP17T
title: "Hosted lake: turn on the metrics listener"
type: chore
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/ops
assignees: []
milestone: null
parent: TKT-01M3K45MQBRESGG5HEZZSZ3FDQ
origin: null
dependencies:
  - TKT-01M3KA70114Q6WFTAAKN5NKG2M
  - TKT-01M3KA702JPYHHZWAFA8BE5RK3
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-28T06:11:10Z
updated_at: 2026-09-28T06:11:10Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/e4a47e8c
  name: ""
extensions: {}
---

## Description

Enable the Prometheus listener on the hosted lake once the queue metrics
and the local status view have landed.

- Add `LAMPI_SERVE_METRICS_ADDR=127.0.0.1:9187` to
  `/etc/terva-lampi/serve.env`.
- Add `--metrics-addr=${LAMPI_SERVE_METRICS_ADDR}` to the serve unit's
  drop-in, which overrides the command line.
- Restart serve and check `/metrics` from the host.

The owner runs the sudo steps. Nothing is exposed off loopback.

## Acceptance criteria

- [ ] the hosted lake serves /metrics on 127.0.0.1:9187
- [ ] the normalization metrics read as expected from the host
