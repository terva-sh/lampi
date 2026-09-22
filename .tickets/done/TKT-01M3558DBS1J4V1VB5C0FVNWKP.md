---
schema: 3
id: TKT-01M3558DBS1J4V1VB5C0FVNWKP
title: Per-path watermark store
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
dependencies:
  - TKT-01M3558DB2BJEEYN3G8M5GAJ17
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

Watermarks keyed by (machine_id, harness, root_path, relative_path): last uploaded size, mtime, content sha256, byte-offset for append-only.

## Acceptance criteria

- [x] Updated only after manifest ACK
- [x] Drives tail-only uploads

## Implementation plan

Fill internal/watermark with a SQLite store keyed by (machine_id, harness, root_path, relative_path). The record is size, mtime, content sha256, and the append-only byte offset.

Commit is the only write. It refuses an empty manifest ACK and does not touch the row. Plan uses that record to choose unchanged, a tail starting at the stored offset when the prefix hash matches, or a full replace after truncate or a non-prefix rewrite. Sync is not rewired in this change; the daemon and redact tickets still own that path.

## Summary

internal/watermark stores one row per (machine_id, harness, root_path, relative_path): size, mtime, content sha256, and byte offset. Commit writes only when the manifest ACK has a session uid. Plan returns a tail starting at that offset when the prefix hash matches, and a full replace after truncate or a non-prefix rewrite. Sync does not call Commit yet.
