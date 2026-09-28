---
schema: 3
id: TKT-01M3M7M0RQKWEVAZX1K6RD270P
title: "Agent heartbeat: durable last contact, sync counters, applied profile"
type: task
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area/protocol
  - area/catalog
  - area/agent
assignees: []
milestone: null
parent: TKT-01M3M7KB32E2BCFEA9CN710522
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-28T14:45:05Z
updated_at: 2026-09-28T15:35:40Z
created_by:
  id: agent:claude-code/2cf53976
  name: ""
updated_by:
  id: agent:claude-code/2cf53976
  name: ""
extensions: {}
---

## Description

Agents tell the lake they are alive and what their last sync did. Today the lake keeps last contact only in memory (`internal/api/server.go` contacts map), lost when `serve` restarts, and the sync counters exist only in the agent's `last_sync.json`.

- New authenticated endpoint, e.g. `POST /v1/agent/report`, sent after each sync and at least every few minutes while idle.
- Body: agent version, inventory mode (SOCIABLE or STRICT), applied profile name and version, where each part of the effective config came from (`allow_source`, `deny_source`, as `agent config` prints them), last sync time and counters (checked, missing, uploaded, manifests, refused, quarantined, unchanged), and the last error.
- The lake stores the newest report per device in the catalog, including a durable `last_seen`.
- The Machines table on `/operations` reads from the catalog rather than from memory.

`allow_source` is important: a machine with local allow rules ignores the lake's allow list (`ApplyLakeProfile`), and the dashboard has to say so, or an edit that has no effect looks broken.

## Acceptance criteria

- [x] Agents report after each sync and periodically when idle
- [x] The lake stores last_seen and the newest report per device durably
- [x] The operations Machines table reads last contact from the catalog

## Implementation plan

Three PRs, each stacked on the one before.

1. **Lake side** (`config/heartbeat-catalog`):
   - `protocol.AgentReport` and `POST /v1/agent/report`.
   - A `device_reports` table (migration 12) holding one row per device: `received_at` and the report as JSON. Each report replaces the previous one.
   - The handler clamps every string field and does not keep unknown fields.
2. **Agent side:**
   - `upload.PostReport` in `transport.go`.
   - The report goes out after each sync (`lakeRunner.run`, after `runAgentSync`) and on a 5-minute idle ticker started beside `watchProfile`.
   - `LastSync` gains `unchanged`. The last error and its time are kept in the runner and cleared by a successful sync.
   - The applied profile comes from `lakeprofile.Load`, and the sources from `Lake.AllowFrom` and `DenyFrom()`.
3. **Readers:**
   - The operations Machines table and the `lampi_device_last_contact_timestamp_seconds` gauge show the newer of the in-memory contact and the catalog's `received_at`.

The report is stored as JSON rather than in columns so the report can grow new fields (the inventory mode, and the applied version for push) without a migration each time.

`last_seen` is written only when a report arrives, not on every authenticated request. A write on every request would add a catalog write to every blob upload. The in-memory contact still covers the time between reports.

## Notes

**agent:claude-code/2cf53976** at 2026-09-28T15:25:27Z

Slice 3 changed from the plan. It does not add a catalog read to each reader. Instead, api.Open seeds the in-memory contact map from device_reports.received_at, and noteContact keeps it current after that. The Machines table and the lampi_device_last_contact_timestamp_seconds gauge get the durable value without changing, and the value still updates on every request. The rejected approach, reading device_reports in both readers and taking the newer time, would have duplicated the merge logic and added a catalog query to each metrics scrape.

**agent:claude-code/2cf53976** at 2026-09-28T15:35:40Z

Review outcomes:
- #66, review 1170 (and 1168/1169 before the restack): medium finding-1, an older concurrent report could replace a newer one. Accepted and fixed in 8441b12: received_ns INTEGER plus a conditional upsert.
- #67, review 1171: medium finding-1, the shutdown drain was not recorded in the report. Declined: after ctx ends there is no delivery path, and the next start reports afresh.
- #68 review: clean.
Dispositions were posted as PR comments, and reviews were re-dispatched on the fixed heads.

## Summary

Three stacked PRs.
- #66: POST /v1/agent/report and a device_reports table (migration 12) holding each device's newest report as JSON, with fields clamped.
- #67: the agent reports after every sync and every 5 minutes, with its version, machine, cached profile and version, allow/deny sources, last sync counters (last_sync.json gains unchanged) and newest error. A lake from before reports is named once in the log.
- #68: serve seeds its in-memory last contact from the stored reports, so the operations Machines table and the last-contact gauge survive a restart.
The inventory mode field is in the protocol but left empty until TKT-01M3M7M0TH.
