---
schema: 3
id: TKT-01M3558DVTRM3ZFEYXNH191KES
title: Claude Code JSONL adapter
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/adapter
  - phase/2-harness
assignees: []
milestone: phase-2
parent: TKT-01M3558DV5NHYZBFMPV1AVRNYQ
origin: null
dependencies:
  - TKT-01M3558DPVCN661600P3F9HMB0
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-22T18:15:11Z
updated_at: 2026-09-23T12:27:00Z
created_by:
  id: human:sothr
  name: Drew Short
updated_by:
  id: agent:cursor/e4d5
  name: Cursor cloud agent
extensions: {}
---

## Description

Discover/watch $CLAUDE_CONFIG_DIR/projects/**/*.jsonl. Treat record shape as internal; pin adapter version; preserve unknown fields.

## Acceptance criteria

- [ ] Discovers and watches $CLAUDE_CONFIG_DIR/projects/**/*.jsonl
- [ ] Adapter version is pinned and the record shape is treated as internal
- [ ] Unknown fields are preserved
