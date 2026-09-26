---
schema: 3
id: TKT-01M3FP1107KXYARCAYVYT2Y409
title: "Client state: per-lake directories and one-time legacy migration"
type: task
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area/agent
assignees: []
milestone: null
parent: TKT-01M3FHHBCJ12FXKNTB6Z138F6N
origin: null
dependencies:
  - TKT-01M3FHHBN7G90MR0BJ89DGPRQQ
blocks_on: none
references: []
claim:
  actor: agent:claude-code/e4a47e8c
  branch: onboarding/per-lake-state
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-e4a47e8c
  commit: 6cc3707ab21413f7b17a9f542f83432c4a518ff5
  session: null
  claimed_at: 2026-09-26T20:55:01Z
  expires_at: null
archive: null
created_at: 2026-09-26T20:20:39Z
updated_at: 2026-09-26T20:55:01Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/e4a47e8c
  name: ""
extensions: {}
---

## Description

Part of the agent onboarding epic. Move per-lake client state under a directory per lake, and migrate existing installs once.

Per-lake state moves to `StateDir/lakes/<lake-id>/`: `watermarks.db`, `outbox.db`, `last_sync.json`, `last_attempt.json` and `machine.json`. State that does not depend on the lake stays shared: `quarantine.jsonl`, `quarantine_allow.json` and `agent.pid`. A legacy lake with no lake id yet (one that has not been upgraded) uses a directory named after its local name until the lake id is known.

On the first start of the new binary, the legacy files move under the `default` lake. The existing `machine.json` becomes that lake's machine id, so the hosted lake keeps its provenance. The move is crash-safe and the old files stay until it has committed.

`serve` without `--data` uses the same state directory (`internal/cli/serve.go`, the `config.StateDir` default), and the workstation this is developed on runs both. The migration moves only the named client files, by an explicit list, and never touches `catalog.db`, the object store or anything else a lake writes.

Tests use the isolated XDG fixture in `internal/cli/golive_test.go`. Migrate a copy of a real legacy layout, including one that shares its directory with a lake's data, and prove that the lake files are untouched and that a second sync after migration uploads nothing.

## Acceptance criteria

- [x] Per-lake state lives under StateDir/lakes/<lake-id>/ and shared state stays shared
- [x] A legacy state directory migrates crash-safely into the default lake and keeps its machine id
- [x] The migration never touches a lake's files in a shared directory, proven by a test
- [x] A sync after migration uploads nothing

## Implementation plan

New internal/lakestate: Dir(state, name) = state/lakes/<name>. Migrate(state, name) copies watermarks.db and outbox.db with VACUUM INTO and last_sync/last_attempt by copy into lakes/.<name>.migrating, fsyncs, renames into place, then removes the legacy files and their -wal/-shm/-journal by name. It never lists the directory, so catalog.db, cas/, identity.json, quarantine files and agent.pid are untouched. upload.Options gains LakeStateDir for outbox, watermarks, last sync and attempt; quarantine stays in StateDir. CLI: prepareLake migrates the default lake (taking agent.pid unless the caller holds it, so an older agent is never migrated under) and returns the lake dir and a per-lake machine id (default keeps machine.json; others machines/<name>.json in the config dir). status reads the lake dir, or the single-lake files read-only before the first move. Removes the interim push-only-to-default guard: sync --lake X now works; without --lake, sync and agent push to one lake until fan-out.

## Notes

**agent:claude-code/e4a47e8c** at 2026-09-26T20:55:01Z

Deviation from the ticket text, which said lakes/<lake-id>/: the directory is keyed by the local lake name. A legacy lake's id is unknown until it is upgraded and answers hello, so an id-keyed directory needs a second rename later, and a name is already validated as one safe path segment. Cost: renaming a lake in config.json starts it with empty state, which re-checks files against the lake; blobs and manifests are idempotent there. machine ids live in the config dir, not state, because machine.json already does and losing it splits a machine's history. Rejected: moving the SQLite files with rename, because a crash between the .db and its -wal renames loses committed transactions; VACUUM INTO folds the WAL into the copy.
