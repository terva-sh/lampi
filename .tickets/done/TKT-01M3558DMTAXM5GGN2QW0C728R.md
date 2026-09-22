---
schema: 3
id: TKT-01M3558DMTAXM5GGN2QW0C728R
title: Normalize terva raw → schema_version 1 events
type: task
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area/normalize
  - phase/1-mvp
assignees: []
milestone: mvp
parent: TKT-01M3558DM7F4PV6QVHXR8CGFVG
origin: null
dependencies:
  - TKT-01M3558DJXS57VSP73457Z1DGD
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-22T18:15:11Z
updated_at: 2026-09-22T23:42:25Z
created_by:
  id: human:sothr
  name: Drew Short
updated_by:
  id: agent:cursor/66b0
  name: Cursor cloud agent
extensions: {}
---

## Description

Fill normalize stub: raw blob → normalized events per research schema §5.1. Failures leave raw intact; mark normalize_error.

## Acceptance criteria

- [x] terva raw becomes schema_version 1 events
- [x] Unknown fields are kept and encrypted_content stays opaque
- [x] A normalize failure leaves the raw blob and records normalize_error

## Implementation plan

Project a terva JSONL blob into schema_version 1 events in internal/normalize. One event per meta, usage, compaction, and unknown row, and one per content block (text, reasoning, tool_call, tool_result, compaction_summary). session_id is harness plus the native id. Unknown keys are copied into extra. encrypted_content is copied as a string and is not placed in content_text.

The manifest handler runs that projection after catalog ingest. A failure sets sessions.normalize_error, deletes any derived JSONL for that session, and does not open the CAS object for write. A later success clears the error and writes normalized/<session_uid>.jsonl.

## Summary

internal/normalize.Terva projects a terva JSONL blob into schema_version 1 events. session_id is terva: plus the native id. Unknown keys land in extra. encrypted_content is copied as a string and is not written into content_text.

POST /v1/manifests runs that projection after the catalog insert. A failure sets sessions.normalize_error, removes normalized/<session_uid>.jsonl, and does not write the CAS object. The ACK is still 200. A later success clears the error.
