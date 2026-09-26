---
schema: 3
id: TKT-01M3DMVEJS3VDHRJZVJF3TYW9H
title: "Flaky agent cancel test: start pass sees nothing under load"
type: bug
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/agent
  - area/ci
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-26T01:21:39Z
updated_at: 2026-09-26T01:21:43Z
created_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
extensions: {}
---

## Description

`TestAgentCancelSkipsFailedSyncRetry` in `internal/cli/agent_test.go` fails intermittently. It failed once in Forgejo CI (run 21, on PR #3, a docs-and-tickets change with no Go code), and the same test passed on `main` (run 17). Locally, `go test -race -count=200` passes 200 of 200 runs. With every core busy under a spin loop and `-cpu 1,2 -count=150`, it failed 2 times in 300.

### The failing output

```
sessions: 1
watch: fsnotify
watching
checked 0, missing 0, uploaded 0, manifests 0, refused 0, quarantined 0, unchanged 0
terva-lampi: draining outbox
drain: checked 0, ...
terva-lampi: drain: upload: POST /v1/hello: 503 Service Unavailable: down
agent_test.go:223: hellos 1
```

### What it shows

- The start sync reported `checked 0` and returned without an error or a hello, although the fixture plants one session in the allowlist. The test assumes this pass is the failed push that makes the first hello.
- `waitOut` fails the test if no `503` appears within 15 seconds, so a 503 was in the output before `cancel()`. The only 503 in the output is the drain's. So the loop drained without a cancel. Of the branches in `runAgentLoop` (`internal/cli/agent.go`), only the `watchErr` case does that, which means a watcher's `Run` returned early.

These are two separate anomalies: a start pass that sees nothing, and a watcher that stops. Either could be a real agent bug under load and not only a test-timing problem. Both code paths arrived with #75 to #77 (agent resilience and incremental sync).

### Reproduce

```sh
export XDG_CONFIG_HOME=$(mktemp -d) XDG_STATE_HOME=$(mktemp -d)
# load every core, e.g. one `sh -c 'while :; do :; done'` per CPU, then:
go test -race -cpu 1,2 -count=150 -run 'TestAgentCancelSkipsFailedSyncRetry$' ./internal/cli/
```

## Acceptance criteria

- [ ] The start pass's empty result and the early watcher stop are explained, and the agent is fixed if either is an agent bug
- [ ] The test passes 300 of 300 runs under the loaded reproduction above
