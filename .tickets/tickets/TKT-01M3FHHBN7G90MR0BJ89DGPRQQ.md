---
schema: 3
id: TKT-01M3FHHBN7G90MR0BJ89DGPRQQ
title: "Client config: lakes map with legacy default and --lake selection"
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
  - TKT-01M3FHHBDS7VCKK5AJ7T3H6DYX
blocks_on: none
references: []
claim:
  actor: agent:claude-code/e4a47e8c
  branch: onboarding/client-lakes
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-e4a47e8c
  commit: 4ed14b07aacd20a1267908bd1e04eddac23c4c8e
  session: null
  claimed_at: 2026-09-26T20:42:22Z
  expires_at: null
archive: null
created_at: 2026-09-26T19:02:11Z
updated_at: 2026-09-26T20:42:23Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/e4a47e8c
  name: ""
extensions: {}
---

## Description

Part of the agent onboarding epic. Change the client configuration from one lake to a set of lakes. The state layout is the next child.

`config.json` gains a `lakes` map keyed by a local name. Each entry holds the server URL, token file, pinned lake id and public key, and that lake's `projects` rules. Top-level `projects.deny` and `redaction` apply to every lake. The legacy top-level `server` and `token_file`, and `LAMPI_SERVER` and `LAMPI_TOKEN_FILE`, keep working as one lake named `default`, so current machines and the deploy examples need no edit. A `lakes` entry named `default` together with a legacy `server` is an error that names both.

`--lake NAME` selects one lake on `sync`, `status` and `conflicts`. `terva-lampi agent config` prints each lake with the source of each value. A lake's allow rules apply only to uploads to that lake, and a top-level deny wins over every lake's allow.

Today the resolution is `ResolveServer` and `ResolveTokenFile` in `internal/config/config.go`, and `loadAgent` in `internal/cli/agent.go` builds one `upload.Options`. This child returns a list of per-lake options, and the agent still uses only the first until the fan-out child.

## Acceptance criteria

- [x] config.json accepts a lakes map, and the legacy server, token_file and env vars still resolve as the lake named default
- [x] A lake's allow rules apply only to that lake and a top-level deny wins over all of them
- [x] sync, status and conflicts accept --lake, and agent config prints each lake with the source of each value

## Implementation plan

config.File gains lakes map[string]LakeConfig (server, token_file, lake_id, key_id, public_key, projects). config.ResolveLakes(file, getenv, LakeFlags{Lake, Server, TokenFile}) returns the lakes, default first then by name. No map: one legacy default lake resolved exactly as ResolveServer/ResolveTokenFile did. With a map: top-level server/token_file or LAMPI_* still add the default lake; an explicit lakes.default beside them is an error; top-level allow belongs to the legacy default and is an error without one; top-level deny joins every lake's deny. Flags apply to the --lake lake or the default; with several lakes they need --lake. CLI: --lake on sync, status, conflicts; agent config prints a lake line per lake. Guard until per-lake state: sync and agent push only to the default lake (warn about others, refuse another --lake, refuse a map-only config) because every lake would share one watermark store.

## Notes

**agent:claude-code/e4a47e8c** at 2026-09-26T20:42:23Z

Interim guard, removed by TKT-01M3FP110 (Client state: per-lake directories and one-time legacy migration): sync and the agent push only to the default lake. Rejected: letting sync --lake work push with the shared state dir, because watermarks.db has no lake key, so files already sent to default would read as sent and never reach work, and the stale-offset logic would record one lake's head size for the other. Rejected: blocking this child on per-lake state, because the resolution layer is independently testable and the next child needs it first. Evidence: internal/config/lakes_test.go (legacy parity with ResolveServer, scoping of allow, top-level deny on every lake, selection, env, error cases) and internal/cli/lakes_test.go (two in-process lakes: default refuses a work-only session, sync --lake work refused with the ticket named, status/conflicts --lake reach work with its token, agent config lines). go test -race ./... green.
