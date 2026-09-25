---
schema: 3
id: TKT-01M3B369JQHQK0ES07FEP1RX5B
title: "Operator commands: backup, purge, fsck, read-only export"
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/ops
  - area/server
  - area/auth
assignees: []
milestone: null
parent: TKT-01M3B35JS8J2F83FG4J7ZC0199
origin: null
dependencies:
  - TKT-01M3B368ZXNN3AN518CGT3YSZ0
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-25T01:34:31Z
updated_at: 2026-09-25T01:41:57Z
created_by:
  id: agent:claude-code/eh1m
  name: Claude Code cloud agent
updated_by:
  id: agent:claude-code/eh1m
  name: Claude Code cloud agent
extensions: {}
---

## Description

Operator commands the lake needs soon after go-live.

### Findings

- `export` runs a second lake. Read. `internal/cli/export.go:79` calls `api.Open`, which loads `normalize_jobs` and starts workers. The generation check and `lockSession` hold within one process, so an older generation can overwrite newer derived files. Contention can also fail a serve write transaction, because a deferred transaction's upgrade returns `SQLITE_BUSY` without the `busy_timeout` retry.
- No purge. `docs/policy.md` says bytes stay until an explicit manual purge, and there is no purge command. A secret that slips past the scan is manual SQL and file work.
- No cleanup or fsck. Stale `.put-*` temp files and abandoned `cas/partial/*` stay forever. Nothing re-hashes stored objects.
- Normalize worker. A transient catalog error drops the job from memory until the next restart: `runNormalize` returns without a requeue when `NormalizeGen` or `Session` errors (`internal/api/worker.go:151-157`, `:162-165`). A `StoreEvents` failure is retried three times and then dropped the same way (`:166-172`). A transient CAS read error becomes a permanent `normalize_error`. `s.pubs` grows by one mutex per session.
- Tokens. A token change needs a restart, which cuts uploads. A `#label` line in the token file became a working token, and in directory mode `laptop~` or `laptop.revoked` stays enrolled (`loadDeviceDir` and `loadDeviceFile`, `internal/auth/devices.go:112-182`). Proven.
- `make build` does not stamp a version; `just build` does.

### Approach

`serve backup` (`VACUUM INTO` plus the CAS copy order). `export` opens the catalog read-only without workers, or refuses while serve holds a lock file; `Ingest` uses `BEGIN IMMEDIATE`. `purge --session <uid>` removes catalog rows, derived files, and blobs no other artifact names. A start-up sweep of temp and partial files older than a day. `serve fsck` re-hashes objects. Retry transient normalize errors. Plaintext tokens must be 64 lowercase hex, `#` lines are comments kept through the rewrite, directory mode loads one suffix, and SIGHUP reloads. Stamp the version in the Makefile.

## Acceptance criteria

- [ ] serve backup writes a consistent catalog copy
- [ ] export against a live lake does not start workers
- [ ] purge removes a session and its unreferenced blobs
- [ ] fsck re-hashes objects and names bad ones
- [ ] Token files accept only 64-hex tokens and # comments, and SIGHUP reloads them
