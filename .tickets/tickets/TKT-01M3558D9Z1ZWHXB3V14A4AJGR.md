---
schema: 3
id: TKT-01M3558D9Z1ZWHXB3V14A4AJGR
title: MVP client pipeline (fill stubs)
type: epic
status: ready
status_reason: null
priority: urgent
due_on: null
labels:
  - area/agent
  - phase/1-mvp
assignees: []
milestone: mvp
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-22T18:15:11Z
updated_at: 2026-09-22T18:15:12Z
created_by:
  id: human:sothr
  name: Drew Short
updated_by:
  id: human:sothr
  name: Drew Short
extensions: {}
---

## Description

Implement the local agent path for terva JSONL: watch → redact → chunk/hash → durable outbox → watermarks → upload. Scaffold PR #1 left stubs only.

## Acceptance criteria

- [ ] fsnotify/poll watcher tracks byte offset for append-only JSONL
- [ ] Outbox + watermarks survive reboot
- [ ] Redaction v1 runs before any network upload
- [ ] `terva-lampi agent` and `sync` share the pipeline

## Definition of done

- [ ] Child tickets 010–016 done
- [ ] Covered by MVP acceptance suite (040)
