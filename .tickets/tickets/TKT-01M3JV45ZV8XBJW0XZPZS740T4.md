---
schema: 3
id: TKT-01M3JV45ZV8XBJW0XZPZS740T4
title: "Web: operations page with storage, growth, queues and devices"
type: task
status: ready
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
updated_at: 2026-09-28T01:47:35Z
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

- [ ] Operations page and /api/web/v1/operations show storage now and growth charts
- [ ] Process, queue and device freshness figures are shown
- [ ] Docs describe every number and what it is not
