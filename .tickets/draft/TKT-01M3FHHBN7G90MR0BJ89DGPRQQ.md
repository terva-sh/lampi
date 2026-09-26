---
schema: 3
id: TKT-01M3FHHBN7G90MR0BJ89DGPRQQ
title: "Client: lakes map in config.json and per-lake state, with migration"
type: task
status: draft
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
  - TKT-01M3FHHBDS7VCKK5AJ7T3H6DYX
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-26T19:02:11Z
updated_at: 2026-09-26T19:02:11Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/e4a47e8c
  name: ""
extensions: {}
---

## Description

Part of the agent onboarding epic. Change the client config and state layout from one lake to a set of lakes, and migrate existing installs.

### Config

`config.json` gains a `lakes` map keyed by a local name. Each entry holds the server URL, token file, pinned lake id and public key, and that lake's `projects` rules. Top-level `projects.deny` and `redaction` apply to every lake. The legacy top-level `server` and `token_file`, and `LAMPI_SERVER` and `LAMPI_TOKEN_FILE`, keep working as one lake named `default`, so current machines and the deploy examples need no edit. `--lake NAME` selects one lake on `sync`, `status` and `conflicts`.

### State

Per-lake state moves to `StateDir/lakes/<lake-id>/`: `watermarks.db`, `outbox.db`, `last_sync.json`, `last_attempt.json` and `machine.json`. State that does not depend on the lake stays shared: `quarantine.jsonl`, `quarantine_allow.json` and `agent.pid`. On the first start of the new binary, the legacy files move under the `default` lake. The move is crash-safe and the old files stay until it has committed. The existing `machine.json` becomes that lake's id, so the hosted lake keeps its provenance.

Tests use the isolated XDG fixture in `internal/cli/golive_test.go`. Migrate a copy of a real legacy state layout, and prove that a second sync after migration uploads nothing.

## Acceptance criteria

- [ ] config.json accepts a lakes map, and the legacy server, token_file and env vars still resolve as the lake named default
- [ ] Per-lake state lives under StateDir/lakes/<lake-id>/ and shared state stays shared
- [ ] A legacy state directory migrates crash-safely, and the next sync uploads nothing
- [ ] sync, status and conflicts accept --lake
