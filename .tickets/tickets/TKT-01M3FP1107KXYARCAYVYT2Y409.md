---
schema: 3
id: TKT-01M3FP1107KXYARCAYVYT2Y409
title: "Client state: per-lake directories and one-time legacy migration"
type: task
status: ready
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
claim: null
archive: null
created_at: 2026-09-26T20:20:39Z
updated_at: 2026-09-26T20:20:46Z
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

- [ ] Per-lake state lives under StateDir/lakes/<lake-id>/ and shared state stays shared
- [ ] A legacy state directory migrates crash-safely into the default lake and keeps its machine id
- [ ] The migration never touches a lake's files in a shared directory, proven by a test
- [ ] A sync after migration uploads nothing
