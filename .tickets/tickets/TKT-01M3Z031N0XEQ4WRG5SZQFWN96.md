---
schema: 3
id: TKT-01M3Z031N0XEQ4WRG5SZQFWN96
title: Normalize core ACP for Grok Build
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/normalize
  - phase/2-harness
assignees: []
milestone: null
parent: null
origin: null
dependencies:
  - TKT-01M3Z02QJNXSYB4XQHPJGD9R59
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-02T19:05:08Z
updated_at: 2026-10-02T19:05:18Z
created_by:
  id: agent:cursor/6699
  name: Cursor cloud agent
updated_by:
  id: agent:cursor/6699
  name: Cursor cloud agent
extensions: {}
---

## Description

Project a stored Grok Build `updates.jsonl` onto schema_version 1. `session_id` is `grok:` plus the native session UUID. `updates.jsonl` is the ACP event stream and the source of truth. `chat_history.jsonl` is not the transcript. `summary.json` is session metadata (cwd, title, model), not the event list.

This waits on the discover/watch/manifest ticket. It does not change Cursor projectors, and it does not upload full sidecars or promote usage.

## Acceptance criteria

- [ ] A stored grok transcript_jsonl projects onto schema_version 1 events
- [ ] session_id is grok: plus the native session UUID
- [ ] chat_history.jsonl is not read as the transcript
- [ ] summary.json is not projected as the event stream
- [ ] Cursor projectors are unchanged

## Definition of done

- [ ] api.Server.Project dispatches harness grok
- [ ] A test covers one ACP updates.jsonl fixture
- [ ] go test ./... is green
