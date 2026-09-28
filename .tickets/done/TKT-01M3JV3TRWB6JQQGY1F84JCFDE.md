---
schema: 3
id: TKT-01M3JV3TRWB6JQQGY1F84JCFDE
title: "Operations dashboard: storage, health, dark mode, design pass"
type: epic
status: done
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
claim: null
archive: null
created_at: 2026-09-28T01:47:17Z
updated_at: 2026-09-28T03:33:10Z
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

## Summary

Every piece of the operations dashboard has landed:

- #49: storage samples by component, with filesystem capacity and
  deduplicated bytes
- #50: the `/operations` page and API, covering storage, growth,
  queues, process, and machine freshness
- #52: Prometheus metrics on an opt-in loopback listener
- #51: a dark theme following the system, with a toggle, and WCAG AA
  tested in both themes
- TKT-01M3JV464: four design directions proposed; the owner chose Field
  Notebook, implemented in #53

Released as v0.1.2 and deployed to the hosted lake afterwards; see
TKT-01M3FP11A for the rollout.
