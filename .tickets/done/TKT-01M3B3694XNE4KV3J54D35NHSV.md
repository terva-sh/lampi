---
schema: 3
id: TKT-01M3B3694XNE4KV3J54D35NHSV
title: "Catalog: schema version, tail retry, manifest checks, recover"
type: bug
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area/catalog
  - area/server
assignees: []
milestone: null
parent: TKT-01M3B35JS8J2F83FG4J7ZC0199
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-25T01:34:31Z
updated_at: 2026-09-25T01:47:39Z
created_by:
  id: agent:claude-code/eh1m
  name: Claude Code cloud agent
updated_by:
  id: agent:claude-code/eh1m
  name: Claude Code cloud agent
extensions: {}
---

## Description

The catalog has no schema version, and ingest has four small correctness gaps. All are cheap before real rows exist.

### Findings

- No schema version. Read. `migrate` (`internal/catalog/catalog.go:164-200`) detects columns ad hoc and `user_version` is 0. An older binary on a newer database is not detected. Per-connection pragmas are set with `db.Exec` (`catalog.go:94-101`), so a reopened connection would lose `busy_timeout`.
- A retried tail is a 409. Proven. `internal/api/merge.go:92-99` compares the stored length with `byte_watermark_prev` before it checks `a.SHA256 == prev.SHA256`. A client whose ACK was lost after the commit gets `409 prefix mismatch` and re-uploads the whole file. Swapping the two checks returns `unchanged` and the api suite stays green.
- Manifests are barely validated. Proven. `size` is not compared with the blob when `byte_watermark_prev` is 0, so `size:-5` came back as `head_size:-5`. `harness` and `kind` are not checked against the known sets, so `harness:"../../etc"` was stored. Parquet paths only use allowlisted harnesses, so there is no traversal today.
- A normalize panic crash-loops serve. Read; fuzzing about 1.3M inputs across the six normalizers found no panic today. The repo has no `recover()`. A panic in `internal/api/worker.go` kills the process, the job row reloads at start, and `Restart=on-failure` restarts into the same panic.

### Approach

Numbered migrations recorded in `PRAGMA user_version`, and refuse to open a newer database. Pragmas in the DSN. Swap the merge checks. Reject a size that does not match the blob, and an unknown harness or kind. `recover` in `runNormalize`, record `normalize_error`, drop the job.

## Acceptance criteria

- [x] The catalog records PRAGMA user_version and refuses a newer database
- [x] A retried, committed tail manifest returns unchanged, not 409
- [x] A manifest whose size does not match the blob, or with an unknown harness or kind, is refused
- [x] A panic in a normalize worker sets normalize_error and serve keeps running

## Implementation plan

### Schema version

`catalog.Open` reads `PRAGMA user_version`. A list of numbered migrations moves version N to N+1, each in one transaction that also sets `user_version`. Migration 1 is today's `schema` plus the ad hoc `migrate` reshapes, so a pre-versioning file (version 0, any older shape) lands on version 1. A file whose version is above the binary's refuses to open with an error naming both numbers.

### Pragmas

`busy_timeout`, `foreign_keys`, `journal_mode`, and `synchronous` move into a `file:` URI DSN as `_pragma=` parameters, so every connection the pool opens gets them. The path is made absolute and escaped by `url.URL`. `synchronous` is FULL: the ACK follows the commit. outbox and watermark keep their `db.Exec` pragmas; they are client-side and out of scope.

### Merge

In `resolve`, a tail whose digest equals the stored head is skipped before the length check. In `clientBytes`, that same digest returns the stored bytes, and a size that does not match them is a 400. A whole-file artifact whose size does not match the blob is a 400. `validateManifest` refuses a negative size and a harness or kind outside allowlists built from the `protocol` constants.

### Recover

`runNormalize` defers a recover. It logs the panic with its stack, takes the session lock, and when the gen is still current removes the derived files, records `normalize_error`, and deletes the job. The worker loop keeps running.

### Tests

Retried committed tail returns unchanged. Bad size, unknown harness, and unknown kind are 400. A panicking `beforeProject` hook sets `normalize_error`, drops the job, and a later session still normalizes. A newer `user_version` refuses to open. A version-0 file migrates to the current version.

## Notes

**agent:claude-code/eh1m** at 2026-09-25T01:47:24Z

Design choices that differ from or add to the proposed approach.

- The recover covers all of `runNormalize`, not only `Project`, so a panic in `StoreEvents` (the parquet writer) is caught too. It takes the session lock and records the failure only when the job's gen is still current, so a newer ingest's outcome is not overwritten. It also removes the derived files, as every other normalize failure does.
- The panic log goes to a package `workerLog` on stderr. `Server` has no logger and server.go was out of scope for this ticket.
- The harness and kind allowlists are maps in `internal/api/merge.go` built from the `protocol` constants. A new kind is one line there.
- A tail retry whose digest is the stored head but whose size is not is a 400 size error, not a 409.
- `synchronous` is FULL, not NORMAL: the ACK follows the commit, so a committed row must survive power loss.
- outbox and watermark still set pragmas with `db.Exec`. Each opens one connection on the client, so the loss is not reachable today; they are left for a separate change.

## Summary

All four criteria are met. catalog.Open runs numbered migrations above PRAGMA user_version (migration 1 is the old migrate, so a version-0 file lands on 1) and refuses a newer file; pragmas are _pragma parameters in a file URI DSN. A retried tail whose digest is the stored head returns unchanged. A size that does not match the blob or stored head, a negative size, or an unknown harness or kind is 400. A panic in runNormalize is logged with its stack, sets normalize_error, removes derived files, and deletes the job when its gen is current; the worker keeps running. Tests: internal/catalog/schema_test.go, internal/api/manifest_checks_test.go, internal/api/worker_panic_test.go. docs/protocol.md and docs/architecture.md updated.
