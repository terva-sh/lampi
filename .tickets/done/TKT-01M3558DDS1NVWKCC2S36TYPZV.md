---
schema: 3
id: TKT-01M3558DDS1NVWKCC2S36TYPZV
title: Long-running terva-lampi agent daemon loop
type: task
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area/agent
  - phase/1-mvp
assignees: []
milestone: mvp
parent: TKT-01M3558D9Z1ZWHXB3V14A4AJGR
origin: null
dependencies:
  - TKT-01M3558DAGH6Z9MFG6GJ1TF0CS
  - TKT-01M3558DB2BJEEYN3G8M5GAJ17
  - TKT-01M3558DBS1J4V1VB5C0FVNWKP
  - TKT-01M3558DCDJ71TN19N5DDY4RSF
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-22T18:15:11Z
updated_at: 2026-09-22T21:04:36Z
created_by:
  id: human:sothr
  name: Drew Short
updated_by:
  id: agent:cursor/e44f
  name: Cursor cloud agent
extensions: {}
---

## Description

Wire discover → watch → redact → outbox → upload into `terva-lampi agent` as a user-level long-running process.

## Acceptance criteria

- [x] Runs until SIGTERM; drains outbox on shutdown best-effort
- [x] Uses machine_id from scaffold config

## Implementation plan

`terva-lampi agent` with no subcommand stays up until SIGTERM or interrupt. It loads `machine_id` with `config.EnsureMachine` (the scaffold file under the config dir) and calls `upload.Sync`, the same allowlist, ruleset v1, watermark, and outbox path as `terva-lampi sync`.

Startup primes one sync so files already on disk are not stuck until the next append. The watcher then runs. Each debounced change primes another sync. Syncs are serialized. A failure is logged and the process keeps watching; the next change tries again.

On SIGTERM the watch context is cancelled, which stops an in-flight sync. A second `upload.Sync` then runs on a fresh timeout, not the cancelled context, so rows left in the outbox are pushed best-effort. A drain error is logged and the process still exits. `status` stays as it is. systemd and launchd units are a later ops ticket.

## Summary

`terva-lampi agent` with no subcommand is the long-running process. It loads `machine_id` with `config.EnsureMachine` and calls `upload.Sync` for files already on disk, then again when the watcher reports growth. SIGTERM cancels that in-flight push and runs `upload.Sync` once more on a fresh timeout so outbox rows are drained best-effort. A drain error is logged and the process still exits.

`status` is unchanged. systemd and launchd stay a later ticket.
