---
schema: 3
id: TKT-01M3558DB2BJEEYN3G8M5GAJ17
title: Durable outbox (SQLite) for pending blobs/manifests
type: task
status: done
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
updated_at: 2026-09-22T19:05:22Z
created_by:
  id: human:sothr
  name: Drew Short
updated_by:
  id: agent:cursor/d943
  name: Cursor cloud agent
extensions: {}
---

## Description

On-disk outbox of pending blob digests + manifest versions. Survives sleep/reboot. Shared by agent and sync.

## Acceptance criteria

- [x] Crash mid-upload leaves work retryable
- [x] Idempotent dequeue after server ACK

## Implementation plan

Fill internal/outbox with a SQLite queue (WAL, mode 0600) at state_dir/outbox.db, shared by later agent and sync callers through Open(path).

Each row is a blob digest and/or a manifest body plus a monotonic version. Re-enqueue of the same identity keeps a single pending row; a higher version replaces the body. Rows stay pending until Ack. Closing the database without Ack (crash, sleep, reboot) leaves them pending on the next Open. Ack deletes the row and a second Ack is a no-op.

## Summary

internal/outbox is a SQLite queue (WAL, mode 0600) at state_dir/outbox.db. A row stays pending until Ack, including across Close and Open, so a crash mid-upload is retried. Ack deletes the row and a second Ack is a no-op. A higher manifest Version for the same identity replaces the pending body. Agent and sync share it by opening that path; sync is not switched over in this change.
