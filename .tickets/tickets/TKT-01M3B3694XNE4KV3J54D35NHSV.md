---
schema: 3
id: TKT-01M3B3694XNE4KV3J54D35NHSV
title: "Catalog: schema version, tail retry, manifest checks, recover"
type: bug
status: ready
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
updated_at: 2026-09-25T01:34:31Z
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

- [ ] The catalog records PRAGMA user_version and refuses a newer database
- [ ] A retried, committed tail manifest returns unchanged, not 409
- [ ] A manifest whose size does not match the blob, or with an unknown harness or kind, is refused
- [ ] A panic in a normalize worker sets normalize_error and serve keeps running
