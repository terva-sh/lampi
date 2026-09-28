---
schema: 3
id: TKT-01M3MC0S3ZS6DH2EZGGR20W59J
title: "Ops: Prometheus scrape config and alert rules for a self-hosted lake"
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/ops
assignees: []
milestone: null
parent: TKT-01M3MC023P4A5H7PTF662QSSM8
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-28T16:01:57Z
updated_at: 2026-09-28T16:03:33Z
created_by:
  id: agent:claude-code/aa1afd80
  name: ""
updated_by:
  id: agent:claude-code/aa1afd80
  name: ""
extensions: {}
---

## Description

`serve --metrics-addr` already serves Prometheus metrics: disk use by component, filesystem free space, queues, each device's last contact and last new data, request counts, build info, and `lampi_catalog_schema_version`. Self-hosters need that turned into something they can deploy.

- **Container networking.** Inside a container, the metrics listener has to be on a non-loopback address, which needs `--metrics-public`. Document that it's safe only on a private network that the scraper shares, and never with a published port. If the flag's name or warning is misleading in that setup, change it.
- **Scrape config.** An example Prometheus scrape configuration, with the compose service name as the target.
- **Alert rules.** A `deploy/prometheus/lampi.rules.yml` with alerts for:
  - low free space on the lake filesystem, and projected days to full;
  - the lake down (`up == 0` or a failing healthcheck);
  - a device that hasn't made contact in N days, with N configurable;
  - normalize or search queue backlog, and failed normalize jobs;
  - a schema version change, as info, so an unattended upgrade is visible.
- **Dashboard.** Optionally, a Grafana dashboard JSON. The built-in operations page (TKT-01M3JV45Z) already covers a lot, so skip it if it adds little.
- **Logs.** Note that request logs are one line per request on stderr and work with Loki or Promtail as they are. Check whether `serve` logs are structured (`slog`) and document the format so people can parse them.

Check the metric names against `internal/api/metrics.go` before writing rules. Don't invent series.

## Acceptance criteria

- [ ] Metrics in a container are documented, including when --metrics-public is safe
- [ ] deploy/ ships an example scrape config and alert rules using only series metrics.go emits
- [ ] The alert rules pass promtool check rules
