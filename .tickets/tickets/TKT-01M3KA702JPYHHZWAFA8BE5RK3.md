---
schema: 3
id: TKT-01M3KA702JPYHHZWAFA8BE5RK3
title: "serve normalize --status: local normalization view"
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
  - area/ops
assignees: []
milestone: null
parent: TKT-01M3K45MQBRESGG5HEZZSZ3FDQ
origin: null
dependencies:
  - TKT-01M3KA70114Q6WFTAAKN5NKG2M
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-28T06:11:10Z
updated_at: 2026-09-28T06:11:19Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/e4a47e8c
  name: ""
extensions: {}
---

## Description

Add `serve normalize --status`. It reads the catalog directly, as the
service user, so it works with serve stopped, over SSH, and in scripts.
It prints:

- per-state session counts
- queued jobs and the oldest pending age
- the last failure's session and message

With `--json` it prints the same object /v1/stats carries, for scripts.

Follows TKT-01M3KA70114Q6WFTAAKN5NKG2M (the /v1/stats normalization object), whose shape it
reuses.

## Acceptance criteria

- [ ] serve normalize --status prints counts, queue, oldest pending age and last failure without serve running
- [ ] --json prints the /v1/stats normalization object
