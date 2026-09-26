---
schema: 3
id: TKT-01M3FHHBN7G90MR0BJ89DGPRQQ
title: "Client config: lakes map with legacy default and --lake selection"
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
  - TKT-01M3FHHBDS7VCKK5AJ7T3H6DYX
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-26T19:02:11Z
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

Part of the agent onboarding epic. Change the client configuration from one lake to a set of lakes. The state layout is the next child.

`config.json` gains a `lakes` map keyed by a local name. Each entry holds the server URL, token file, pinned lake id and public key, and that lake's `projects` rules. Top-level `projects.deny` and `redaction` apply to every lake. The legacy top-level `server` and `token_file`, and `LAMPI_SERVER` and `LAMPI_TOKEN_FILE`, keep working as one lake named `default`, so current machines and the deploy examples need no edit. A `lakes` entry named `default` together with a legacy `server` is an error that names both.

`--lake NAME` selects one lake on `sync`, `status` and `conflicts`. `terva-lampi agent config` prints each lake with the source of each value. A lake's allow rules apply only to uploads to that lake, and a top-level deny wins over every lake's allow.

Today the resolution is `ResolveServer` and `ResolveTokenFile` in `internal/config/config.go`, and `loadAgent` in `internal/cli/agent.go` builds one `upload.Options`. This child returns a list of per-lake options, and the agent still uses only the first until the fan-out child.

## Acceptance criteria

- [ ] config.json accepts a lakes map, and the legacy server, token_file and env vars still resolve as the lake named default
- [ ] A lake's allow rules apply only to that lake and a top-level deny wins over all of them
- [ ] sync, status and conflicts accept --lake, and agent config prints each lake with the source of each value
