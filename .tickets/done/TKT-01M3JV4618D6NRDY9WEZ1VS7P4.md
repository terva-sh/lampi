---
schema: 3
id: TKT-01M3JV4618D6NRDY9WEZ1VS7P4
title: "Serve: Prometheus text metrics on an opt-in listener"
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/ops
  - area/server
assignees: []
milestone: null
parent: TKT-01M3JV3TRWB6JQQGY1F84JCFDE
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-28T01:47:29Z
updated_at: 2026-09-28T02:41:26Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/e4a47e8c
  name: ""
extensions: {}
---

## Description

Expose the lake's operating numbers in the Prometheus text format, so an
existing monitoring stack can scrape them and alert, for example when the
disk is nearly full or a device stops syncing.

The endpoint listens on its own address, set with `--metrics-addr`, and
is off by default. It refuses a non-loopback address unless
`--metrics-public` is given, because it has no authentication.

It exports:

- storage gauges from the latest sample
- queue depths
- per-device seconds since the last accepted update
- request counters by route class and status
- bytes accepted
- build info

It is written by hand in the text format, with no new dependency.

## Acceptance criteria

- [x] --metrics-addr serves /metrics; off by default; non-loopback needs --metrics-public
- [x] Storage, queue, device, request and build metrics are exported

## Notes

**agent:claude-code/e4a47e8c** at 2026-09-28T02:16:44Z

Decisions, with the alternatives each one beat:

- **The text format is written by hand; no client_golang.** The format
  is simple, and this endpoint has a fixed set of families. The
  dependency would bring its own registry and a process collector for
  little gain. A test holds every line to the exposition grammar and
  declares each family once.
- **A separate listener, not a route on the lake mux.** Any route on
  the mux passes through the web or device auth paths. A scraper
  carries neither an OIDC session nor a device token, and giving it a
  device token would let it upload. Its own listener, loopback by
  default with `--metrics-public` as an explicit opt-in, matches how
  exporters are normally deployed.
- **Request counters use route classes (blob_put, manifest, web, and
  so on), not raw paths.** A path label would carry a digest or a
  session id and give every one its own series.
- **Figures come from the catalog on each scrape.** Serve keeps no
  cached copy. The catalog queries are the same ones the operations
  page uses. The help text advises scraping every 15 seconds or slower.
- **Per-device series are labelled by device name only.** Revoked
  devices are left out, so a revoked series goes stale instead of
  alerting forever. Unbound machines are left out as well, because
  their only label would be a machine id.
- **The listener stops from BeforeClose, ahead of the catalog.** This
  is the same pattern as the storage sampler, so no scrape reads a
  closed database.

Checked: a throwaway serve with `--metrics-addr` answered a curl
scrape, refused `0.0.0.0` without `--metrics-public`, and shut down
cleanly.

## Summary

Landed in PR #52. `serve --metrics-addr` serves `GET /metrics` in the
Prometheus text format on its own listener:

- off by default
- loopback only unless `--metrics-public` is also given
- GET and HEAD only

Families cover build info, start time and schema; storage by component,
with the sample time, filesystem size and free space, and artifact
bytes; sessions, total and by normalization state; the audit outbox;
each device's last contact and last new data; and request and
body-byte counters by route class.

The example unit takes `LAMPI_SERVE_METRICS_ADDR`. `deploy/README.md`
suggests alerts.

Review:

- Rejected, with evidence: the claim that the 24-hour cutoff hides
  last-data timestamps. The cutoff only bounds the recent-update count.
- Accepted: GET-only.
