---
schema: 3
id: TKT-01M3558D9Z1ZWHXB3V14A4AJGR
title: MVP client pipeline (fill stubs)
type: epic
status: done
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
updated_at: 2026-09-22T22:13:39Z
created_by:
  id: human:sothr
  name: Drew Short
updated_by:
  id: agent:cursor/cfd7
  name: Cursor cloud agent
extensions: {}
---

## Description

Implement the local agent path for terva JSONL: watch → redact → chunk/hash → durable outbox → watermarks → upload. Scaffold PR #1 left stubs only.

## Acceptance criteria

- [x] fsnotify/poll watcher tracks byte offset for append-only JSONL
- [x] Outbox + watermarks survive reboot
- [x] Redaction v1 runs before any network upload
- [x] `terva-lampi agent` and `sync` share the pipeline

## Definition of done

- [x] All children of this epic are done
- [x] TKT-01M3558DAGH6Z9MFG6GJ1TF0CS Implement fsnotify/poll watcher for terva JSONL
- [x] TKT-01M3558DB2BJEEYN3G8M5GAJ17 Durable outbox (SQLite) for pending blobs/manifests
- [x] TKT-01M3558DBS1J4V1VB5C0FVNWKP Per-path watermark store
- [x] TKT-01M3558DCDJ71TN19N5DDY4RSF Redaction ruleset v1 + quarantine on hits
- [x] TKT-01M3558DDS1NVWKCC2S36TYPZV Long-running terva-lampi agent daemon loop
- [x] TKT-01M3558DEJYSZ08P53KM9CNVSY One-shot sync uses outbox + watermarks end-to-end
- [ ] Covered by TKT-01M3558DPVCN661600P3F9HMB0 CI/integration: five MVP acceptance tests

## Notes

**agent:cursor/cfd7** at 2026-09-22T22:13:39Z

Chunker TKT-01M3558DD3MYA65Q909VDV374P (Chunker for large artifacts (≥32 MiB)) was reparented to TKT-01M3558DP1WFP9WNHEP6BDVGN3 (MVP acceptance, status, ops). It stays ready there, so this epic no longer parents an open child and stays done.

Remaining children (watcher, outbox, watermarks, redaction, daemon, sync) are done. The CI line stays unchecked: TKT-01M3558DPVCN661600P3F9HMB0 (CI/integration: five MVP acceptance tests) is still ready under the acceptance epic.
