---
schema: 3
id: TKT-01M3M7M0TH9PYS59DADPC604P3
title: Agent inventory report of seen projects, gated by inventory mode
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/agent
  - area/protocol
  - area/catalog
assignees: []
milestone: null
parent: TKT-01M3M7KB32E2BCFEA9CN710522
origin: null
dependencies:
  - TKT-01M3M7M0PMGW8JG0Y45RFPY34M
  - TKT-01M3M7M0RQKWEVAZX1K6RD270P
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-28T14:45:05Z
updated_at: 2026-09-28T22:13:22Z
created_by:
  id: agent:claude-code/2cf53976
  name: ""
updated_by:
  id: agent:claude-code/2cf53976
  name: ""
extensions: {}
---

## Description

In SOCIABLE mode the agent reports what it can see, so the dashboard can show each device's sessions and offer to allow a refused project.

- One row per project the harness roots contain: harness, cwd, cwd_hash, normalized git remote, session count, total bytes, newest session time, allowed or refused, and the `Refusal` reason.
- Built from the same scan and the same `Projects.Permitted` check that `agent refused` uses, so the two cannot disagree.
- STRICT mode sends allowlisted rows only (see the policy ticket for the aggregate count).
- Sent as a full snapshot when it changes (compare with a hash of the last one sent), not on every sync.
- The lake stores the newest snapshot per device, replacing the previous one. Paths are metadata, but they are sensitive, so the snapshot is not kept as history.
- Git reads for projects outside the allowlist follow the same confinement as today (see TKT-01M3B60K); the inventory must not open more of a repository than the allowlist check already does.

## Acceptance criteria

- [x] SOCIABLE agents report every project with counts, sizes, paths and verdict
- [x] STRICT agents report allowlisted projects only; profiles cannot change the mode
- [x] The report agrees with agent refused on the same machine

## Implementation plan

Two PRs.

### I1 (config/inventory-lake): protocol and the lake's side

- `POST /v1/agent/inventory` takes an `AgentInventory`:
  - `mode`: sociable or strict.
  - `projects`: one row per project, with the folded git_remote, one cwd plus a count of checkouts, cwd_hash, harnesses, sessions, bytes, newest session time, and allowed or refused with the reason.
  - `refused_sessions` and `refused_bytes` totals.
  - `truncated`.
- The body is capped at 2 MiB and the list at 2000 projects. Strings are clamped.
- A strict inventory that carries refused rows has them dropped by the lake as well. The mode is the agent's promise, and the lake does not keep what it should not have been sent.
- Catalog migration 15 adds `device_inventories`: device_id primary key, received_ns, inventory JSON. Each device keeps only its newest snapshot, with the same newer-wins upsert as device_reports. Paths are sensitive, so there is no history.
- A lake with no device tokens answers as stored and keeps nothing, as for reports.

### I2: the agent

- `config.json` gets `"inventory": "sociable" | "strict"`, default sociable. A profile cannot carry it: strict decoding already refuses the key, and forbiddenKeys gets a clear message.
- Sync computes the rows from the same bundles and the same `Projects.Refusal` it already uses. `upload.Refusals` becomes a filter over those rows, so the inventory and `agent refused` cannot disagree.
- `Result.Inventory` carries the rows out of Sync. The agent hashes the snapshot and posts it only when the hash differs from the last one the lake accepted, recorded in lake state. A 404 from an older lake is noted once.
- The heartbeat's `inventory` field reports the mode.

### Alternatives considered

- **A field in the heartbeat.** Rejected: the report is capped at 64 KiB and is sent every minute; the inventory is larger and changes rarely.
- **A separate scan on a timer.** Rejected: it would read every session twice, and the rows could drift from what Sync saw.
- **Rows per cwd.** Rejected: allow rules name repositories, and `agent refused` already groups by folded remote, then cwd. The inventory keeps that grouping so the two agree, and the dashboard's allow action can write a git_remote rule.

## Notes

**agent:claude-code/2cf53976** at 2026-09-28T21:36:24Z

I1 on config/inventory-lake: POST /v1/agent/inventory with protocol.AgentInventory/InventoryProject, catalog migration 15 device_inventories (newest-wins upsert, no history), clampInventory drops refused rows from a strict inventory and cuts past MaxInventoryProjects with truncated set. Docs in protocol.md. The agent side is I2.

**agent:claude-code/2cf53976** at 2026-09-28T21:45:16Z

I2 is PR #107, stacked on #106. #106 took a review finding: inventories were ordered by arrival, so a late sociable request could replace a newer strict snapshot. The agent now stamps generated_at and the lake keeps the one generated latest (clamped to the lake's now), answering kept. Deviation from the plan: the last-sent hash lives in runner memory, not lake state. A restart resends one inventory, which is harmless under generated_at ordering and saves a state file.

**agent:claude-code/2cf53976** at 2026-09-28T22:13:22Z

Review-driven changes to ordering, all in #106. The lake keeps the inventory with the latest `generated_at`, not the latest arrival:

- A tie keeps the stored one, because the time cannot say which is newer, and the answer's `kept: false` makes the agent send again.
- A `generated_at` after the lake's now counts as now, so a fast clock cannot pin a snapshot.
- One before 1970 counts as 1970, because UnixNano is undefined before 1678 and could wrap past every real snapshot.

In #107 the send-on-change test got a fresh row slice per simulated sync. Production Sync already builds fresh rows each run.

## Summary

Landed in #106 (the lake side) and #107 (the agent side).

- **Lake.** `POST /v1/agent/inventory` stores each device's newest snapshot in `device_inventories` (migration 15), ordered by the agent's `generated_at`. It clamps strings and caps rows, and it drops refused rows from a strict inventory.
- **Agent.** Sync groups every session into inventory rows, allowed and refused, and `agent refused` is now a filter over those same rows. `config.json` `inventory` is sociable or strict, and a profile that names it is refused. The report loop posts the inventory when its hash changes, and says once if the lake answers 404.
- **Docs.** protocol.md, agent.md and allowlist-and-redaction.md.
