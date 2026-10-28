---
schema: 3
id: TKT-01M61NTM5CHJTF47M4QDNWTEEB
title: "Operations page: index size, normalize throughput, rollup lag"
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
parent: TKT-01M61NT6XQ5WA0XZ0AXZEYMHHG
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-28T16:34:53Z
updated_at: 2026-10-28T21:47:29Z
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

## Implementation plan

Index size on disk is already on Operations: the search component of the storage table, with its change over the range. What is missing is history: the queues show only their current depth. Record the normalize queue (pending, failed, from Catalog.NormalizationCounts) and the search index's lag (behind, failed, through an api.Server.IndexLag hook that serve sets when it runs the index) as count measures in each storage sample, which serve takes hourly and after each normalize batch drains. Operations charts sessions waiting to normalize and sessions the index is behind on, beside the disk charts; growth points in the JSON carry all four. The index lag is also the statistics' lag, since the rollup is written with the index. Normalize throughput and duration were left out: the catalog records no job durations, so they would need a schema change for a figure nobody has asked for yet.

## Summary

Landed in #219. Each storage sample now also records the normalize queue (pending, failed) and the search index lag (sessions behind, sessions it could not read), and the Operations page charts both over time beside the disk charts. These were the cheap candidates: counts the lake already computes. Index size on disk was already charted. Normalize throughput and duration are not done, because the lake does not record how long a normalize job takes; the pending chart shows whether the backlog drains, but not how fast.
