---
schema: 3
id: TKT-01M61NTM5CHJTF47M4QDNWTEEB
title: "Operations page: index size, normalize throughput, rollup lag"
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
parent: TKT-01M61NT6XQ5WA0XZ0AXZEYMHHG
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-28T16:34:53Z
updated_at: 2026-10-28T16:38:41Z
created_by:
  id: agent:claude-code/d4b24133
  name: ""
updated_by:
  id: agent:claude-code/d4b24133
  name: ""
extensions: {}
---

## Description

Add lake health figures to the operations page next to the disk charts.

Candidates: search index size on disk, normalize throughput and duration, rollup lag (ready sessions whose usage rollup is behind), and index lag over time rather than only the current count. Pick the ones that are cheap to sample alongside the existing storage samples.
