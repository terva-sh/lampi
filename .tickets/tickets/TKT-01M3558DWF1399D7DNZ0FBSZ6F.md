---
schema: 3
id: TKT-01M3558DWF1399D7DNZ0FBSZ6F
title: Codex CLI rollout JSONL adapter
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
created_at: 2026-09-22T18:15:12Z
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

Watch $CODEX_HOME/sessions/**/rollout-*.jsonl; do not confuse with history.jsonl.

## Acceptance criteria

- [ ] Watches $CODEX_HOME/sessions/**/rollout-*.jsonl
- [ ] history.jsonl is not ingested as a rollout
