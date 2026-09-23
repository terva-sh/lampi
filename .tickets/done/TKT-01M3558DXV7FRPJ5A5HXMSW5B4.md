---
schema: 3
id: TKT-01M3558DXV7FRPJ5A5HXMSW5B4
title: Async normalizer workers + parquet partitions
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/normalize
  - phase/2-harness
assignees: []
milestone: phase-2
parent: TKT-01M3558DV5NHYZBFMPV1AVRNYQ
origin: null
dependencies:
  - TKT-01M3558DMTAXM5GGN2QW0C728R
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-22T18:15:12Z
updated_at: 2026-09-23T15:15:12Z
created_by:
  id: human:sothr
  name: Drew Short
updated_by:
  id: agent:cursor/05ac
  name: Cursor cloud agent
extensions: {}
---

## Description

Move normalize off synchronous MVP path; partition parquet by date/harness.

## Acceptance criteria

- [x] Normalize runs off the synchronous MVP manifest path
- [x] Parquet is partitioned by date and harness

## Implementation plan

POST /v1/manifests stores the catalog row, enqueues a normalize job, and returns the ACK without projecting. Two in-process workers claim jobs, read the catalog head, and call the existing projector. Only terva is implemented; Claude and Codex still record normalize_error once a worker runs. A generation on the session drops a stale publish when a newer ingest is queued. The job row stays until that publish, so a restart finishes it. Shutdown drains the queue.

Derived JSONL stays at normalized/<session_uid>.jsonl. Parquet is written beside it under parquet/date=YYYY-MM-DD/harness=<harness>/<session_uid>.parquet. The date is the UTC day of recorded_at, and a session that spans days has one file in each partition. A failure removes that session's JSONL and parquet files and sets normalize_error. Export waits until the queue is idle, then reads JSONL, and still rebuilds a missing file.

## Summary

POST /v1/manifests stores the session, enqueues a normalize job, and returns the ACK without projecting. Two workers read the catalog head and project it. A generation on the session drops a stale publish when a newer ingest is queued. The job row stays until that publish, and the next process start loads any row still there. Shutdown drains the queue.

JSONL stays at normalized/<session_uid>.jsonl. Parquet is parquet/date=YYYY-MM-DD/harness=<harness>/<session_uid>.parquet. The date is the UTC day of recorded_at, or ingested_at when that is missing. A session that spans days has one file in each partition. The writer is github.com/parquet-go/parquet-go. A failure removes that session's JSONL and parquet and sets normalize_error. Export waits until the queue is idle, then reads JSONL.

terva still projects. Claude and Codex still have no projector; the worker records that, not the ACK handler. internal/accept TestMVPAcceptance passes.

TKT-01M3558DYH3FVT06XBPKWAAMKH (Optional terva hook nudge) is still ready, so the Phase 2 epic TKT-01M3558DV5NHYZBFMPV1AVRNYQ stays ready.
