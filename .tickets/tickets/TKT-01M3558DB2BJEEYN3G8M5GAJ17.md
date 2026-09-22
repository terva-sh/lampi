---
schema: 3
id: TKT-01M3558DB2BJEEYN3G8M5GAJ17
title: Durable outbox (SQLite) for pending blobs/manifests
type: task
status: ready
status_reason: null
priority: urgent
due_on: null
labels:
  - area/agent
  - phase/1-mvp
assignees: []
milestone: mvp
parent: TKT-01M3558D9Z1ZWHXB3V14A4AJGR
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

On-disk outbox of pending blob digests + manifest versions. Survives sleep/reboot. Shared by agent and sync.

## Acceptance criteria

- [ ] Crash mid-upload leaves work retryable
- [ ] Idempotent dequeue after server ACK
