---
schema: 3
id: TKT-01M3FHHBKTMJCHCMQQQJZKQTMF
title: "Lake base config: signed agent profile, fetch, cache and merge"
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
  - area/agent
  - area/protocol
assignees: []
milestone: null
parent: TKT-01M3FHHBCJ12FXKNTB6Z138F6N
origin: null
dependencies:
  - TKT-01M3FHHBFAYKEK0NAXVR91969G
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

Part of the agent onboarding epic. Let a lake publish a standard base configuration that its agents fetch at registration and keep current.

- The lake operator writes a profile file (the default profile, plus named profiles a code can select). Allowed fields: `harnesses`, `agent.debounce` and `agent.debounce_max`, `redaction`, `projects.deny`, and `projects.allow`. Any other field fails the load, the way an unknown harness key does today.
- Each device records its profile, set from the code at registration. `serve devices set-profile NAME PROFILE` changes it, and the device picks up the change at its next fetch.
- `GET /v1/agent/config` returns the profile for the calling device, signed with the lake key, with a version. The registration response carries the same document.
- The agent caches the last verified copy per lake. It refetches on start and on a slow interval. A copy that does not verify against the pinned key is refused and the cached copy stays in use.
- Merge rules: local `config.json` overrides every field. A local deny wins over a lake allow. A lake's `projects` rules apply only to uploads to that lake. `terva-lampi agent config` prints each effective value and whether it came from the local file or from which lake.

## Acceptance criteria

- [ ] The lake serves a signed profile at GET /v1/agent/config, and a profile with a field outside the allowed set fails the load
- [ ] The agent verifies the profile against the pinned key and keeps its last good copy when a fetch fails or the signature is bad
- [ ] A local deny wins over a lake allow, and one lake's rules never apply to uploads to another lake
- [ ] agent config names the source of each effective value
