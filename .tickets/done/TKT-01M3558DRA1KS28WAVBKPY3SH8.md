---
schema: 3
id: TKT-01M3558DRA1KS28WAVBKPY3SH8
title: Implement terva-lampi status (agent + server)
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/ops
  - area/agent
  - phase/1-mvp
assignees: []
milestone: mvp
parent: TKT-01M3558DP1WFP9WNHEP6BDVGN3
origin: null
dependencies:
  - TKT-01M3558DDS1NVWKCC2S36TYPZV
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-22T18:15:11Z
updated_at: 2026-09-22T21:29:07Z
created_by:
  id: human:sothr
  name: Drew Short
updated_by:
  id: agent:cursor/0d82
  name: Cursor cloud agent
extensions: {}
---

## Description

Status shows machine_id, outbox depth, watermarks summary, last sync, server healthz/catalog counts.

## Acceptance criteria

- [x] status reports machine id, outbox depth, a watermark summary, and last sync
- [x] status reports server health and catalog counts

## Implementation plan

Extend terva-lampi status, which already prints the machine id and GET /healthz.

Local lines come from the state directory the agent and sync already share. outbox.Depth and watermark.Summary are read with the existing Open helpers when those files exist; a missing file is zero, not a new database. upload.Sync rewrites last_sync.json when a run finishes, including a refusal or a quarantine, and leaves the previous stamp in place when the lake errors.

Catalog counts are GET /v1/stats, behind the same bearer check as the other /v1 routes. healthz stays free of catalog data. A lake that does not answer is printed; the local lines still appear. agent status prints the same local lines and does not call the lake.

## Summary

terva-lampi status prints the machine id, outbox depth, a watermark summary (paths, committed bytes, newest mark), and the last finished sync. agent status prints those same local lines and does not call the lake.

upload.Sync rewrites state_dir/last_sync.json when a run finishes, including a refusal or a full quarantine. A lake error leaves the previous stamp in place.

GET /v1/stats returns catalog session, artifact, and machine counts behind the same bearer check as the other /v1 routes. GET /healthz still returns only {"status":"ok"}. A lake that does not answer is reported; the local lines are still printed.
