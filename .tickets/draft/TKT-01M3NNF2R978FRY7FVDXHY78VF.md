---
schema: 3
id: TKT-01M3NNF2R978FRY7FVDXHY78VF
title: "Bays: per-bay retention and purge"
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
  - area/cas
  - policy
assignees: []
milestone: null
parent: null
origin: null
dependencies:
  - TKT-01M3NNF21HT08545KY8QX9FHR3
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-29T04:06:18Z
updated_at: 2026-09-29T04:06:18Z
created_by:
  id: agent:claude-code/7859b064
  name: ""
updated_by:
  id: agent:claude-code/7859b064
  name: ""
extensions: {}
---

## Description

Per-bay retention and purge: "this client engagement ended, delete its bay and its data". Bays in TKT-01M3N8KHW5 (Bays: segment one lake and route sessions to a bay) deliberately never delete data: deleting a bay moves its sessions to the default. This ticket decides what deleting a bay's data means when a session is also in other bays, and when a blob is shared with sessions outside the bay (dedup), and how it relates to `serve purge` and the retention policy in `docs/policy.md`.
