---
schema: 3
id: TKT-01M3JV3TRWB6JQQGY1F84JCFDE
title: "Operations dashboard: storage, health, dark mode, design pass"
type: epic
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area/ops
  - area/server
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim:
  actor: agent:claude-code/e4a47e8c
  branch: ops/storage-samples
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-e4a47e8c
  commit: 7c1576afc652fa3f46052ff1e95c0fe20aa34b40
  session: null
  claimed_at: 2026-09-28T02:03:25Z
  expires_at: null
archive: null
created_at: 2026-09-28T01:47:17Z
updated_at: 2026-09-28T02:03:25Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/e4a47e8c
  name: ""
extensions: {}
---

## Description

The dashboard shows what the lake holds but not what running it costs. An
operator can't see how much disk the lake uses, how fast that is growing,
how much room is left, or whether its background queues are keeping up,
without a shell on the host. The UI also has only a light theme, and its
design has not been revisited since the first dashboard landed.

This epic covers:

- sampling disk use by component and charting its growth
- an operations page with the lake's health and queues
- a Prometheus text endpoint for the same numbers
- a dark theme
- a second design pass, which starts with four directions for the owner
  to choose between
