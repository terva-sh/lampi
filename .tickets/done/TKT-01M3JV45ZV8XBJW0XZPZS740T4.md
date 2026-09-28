---
schema: 3
id: TKT-01M3JV45ZV8XBJW0XZPZS740T4
title: "Web: operations page with storage, growth, queues and devices"
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
dependencies:
  - TKT-01M3JV45Y7RJ7S7Y3WWW2WMEKC
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-28T01:47:29Z
updated_at: 2026-09-28T02:29:06Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/e4a47e8c
  name: ""
extensions: {}
---

## Description

Add an Operations page to the dashboard and a matching read under
`/api/web/v1/operations`. It answers "is this lake healthy and how full
is it" in one place:

- **Storage now:** each component's size, filesystem free and total, and
  the age of the sample.
- **Growth:** the total and each component over 7, 30 and 90 days, drawn
  with the same chart code as Activity.
- **Deduplication:** bytes referenced by current and historical artifacts
  against bytes stored in the CAS.
- **Process:** build version, uptime, catalog schema version and lake id.
- **Queues:** the audit outbox, normalization pending and failed, search
  index behind and failed, and CAS temporary files.
- **Devices:** each registered device with the time of its last accepted
  update, so a machine that has stopped syncing stands out.

The page is readable by anyone who can read the dashboard. It holds no
tokens and no host paths.

## Acceptance criteria

- [x] Operations page and /api/web/v1/operations show storage now and growth charts
- [x] Process, queue and device freshness figures are shown
- [x] Docs describe every number and what it is not

## Implementation plan

- `GET /api/web/v1/operations?range=7d|30d|90d` and `/operations` are built
  from one read, `readOperations`, which combines:
  - the newest storage sample
  - growth buckets, each carrying the last sample in its hour or day
  - queues: outbox, normalization, search coverage and uploads
  - process figures from `web.Operations`, which serve supplies
  - machines: devices joined to `MachinesActivity`, with in-memory contacts
- Catalog migration 11 indexes `head_updates(machine_id, received_ns)`.
- `api.Server` records each device's last authenticated request in
  memory. `Contacts()` exposes it.
- `web.New` gains an `ops *Operations` parameter.
- The smoketest seeds 90 days of synthetic samples so the charts have a
  history to draw.

## Notes

**agent:claude-code/e4a47e8c** at 2026-09-28T02:01:29Z

Decisions, with the alternatives each one beat:

- **Last contact is kept in memory, not persisted.** Writing a row on
  every request would add a catalog write to the hot path. Contact only
  matters for "is it talking now", and a restart losing it is
  acceptable as long as the page says so.
- **Show both last contact and last new data.** Provenance and
  head_updates only record new data. A healthy agent with nothing new
  to send would look stale if new data were the only signal.
- **Growth charts reuse the Activity bar chart, with a level summary.**
  Summing levels over buckets means nothing, so the summary gives the
  latest, lowest and highest value instead. A line or area chart would
  show small growth better; that is left to the design pass
  (TKT-01M3JV464).
- **The page is open to viewers, not only operators.** It holds no
  tokens and no paths. Machine ids already appear on session pages.
- **The page does not auto-refresh.** Samples change hourly.
- **Bug found while testing:** a sample just before the window
  truncated into the first bucket, because integer division rounds
  toward zero. It is now guarded, and a test covers it.

## Summary

Landed in PR #50: `/operations` and `/api/web/v1/operations`. They show:

- storage now, by component, with share and change over the range
- filesystem free and total
- deduplication
- growth charts: 7 days hourly, or 30 or 90 days daily
- queues: normalization, search, audit outbox, uploads
- process: version, uptime, schema, lake id
- every device and unbound machine, with last contact, last new data
  and freshness

Supporting pieces:

- Catalog migration 11 indexes head updates by machine.
- `api.Server` keeps each device's latest contact in memory, with a
  compare-and-swap so that racing requests keep the later time.
- Review fixes: that contact race, and an empty machine list returned
  as `[]` rather than null.
- Review rejections, with evidence: an empty range takes the default,
  as on Activity; machines found only in head_updates cannot exist,
  because provenance is written in the same transaction.
