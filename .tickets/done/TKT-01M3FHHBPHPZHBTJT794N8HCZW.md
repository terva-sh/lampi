---
schema: 3
id: TKT-01M3FHHBPHPZHBTJT794N8HCZW
title: "Agent: fan out to many lakes with per-lake outbox, backoff, status"
type: task
status: done
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
  - TKT-01M3FP1107KXYARCAYVYT2Y409
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-26T19:02:11Z
updated_at: 2026-09-27T00:04:01Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/e4a47e8c
  name: ""
extensions: {}
---

## Description

Part of the agent onboarding epic. Run one agent against several lakes.

One process, one watch, one scan per pass. Each pass routes each session through each lake's allowlist and pushes to each lake independently. The outbox, backoff, 401/403 logging and debounce ceiling are per lake, so a lake that is down or refusing the token does not hold back another. Today these are a single `bo` and `authLogged` in `runAgentLoop` (`internal/cli/agent.go`).

`status` prints a block per lake: URL and source, lake id, health, stats, outbox depth, last sync and last error. `sync` pushes to every lake unless `--lake` is given.

## Acceptance criteria

- [x] One agent pushes to each lake by that lake's allowlist, with outbox, backoff and auth logging kept per lake
- [x] A lake that is down or refusing the token does not delay uploads to another lake
- [x] status prints one block per lake, and sync pushes to every lake unless --lake is given

## Implementation plan

loadAgentLakes resolves every lake into options (own token, allowlist, state dir, machine id, memo). runAgentLoop keeps one watch, one debouncer and SIGUSR1, and runs a lakeRunner goroutine per lake, each with its own kick channel, waiting flag, retry timer, backoff, auth-logged flag and refusal log; growth wakes only lakes not waiting, the start pass and SIGUSR1 wake all. Shutdown or a watch failure cancels the runners, each drains its own outbox. Output lines carry 'lake <name>: ' when there are several. sync pushes to every lake in turn (or --lake), continues past a failing lake and exits non-zero naming the failed ones. status prints a block per lake.

## Notes

**agent:claude-code/e4a47e8c** at 2026-09-26T21:14:15Z

Rejected: one loop that pushes to each lake in sequence on each kick, because a lake timing out would delay every other lake by its stall timeout. Rejected: one shared memo, because a file the memo marks as seen after pushing to one lake would be skipped for the next. Cost: each lake hashes the files it admits, so CPU scales with lake count. Evidence: TestAgentPushesToEachLakeAndALockedOutLakeDoesNotBlockTheOther (work receives while default answers 401), TestSyncPushesToEveryLakeAndKeepsGoingPastAFailure, status block test; go test -race ./... and the golive drills green.

**agent:claude-code/e4a47e8c** at 2026-09-27T00:04:01Z

Review 929 on PR #14, both findings fixed in 5667596. (1) sync stopped at a lake it could not prepare; now that lake is recorded as failed and the next one runs, which is safe because each lake has its own state directory. (2) an interrupt between lakes returned success; now the lakes not reached count as failed. The per-lake push goes through var syncLake so a test can land the interrupt between lakes.

## Summary

Lands in PR #14. One agent runs a lakeRunner goroutine per lake behind a single watch and debouncer. Each runner has its own kick channel, retry timer, backoff, auth-logged flag and refusal log, so a lake that is down or answering 401 waits out its own backoff while the others keep receiving. sync pushes to every lake in turn (or only --lake), keeps going past a failing lake, and exits non-zero naming the lakes that failed. status prints one block per lake. When there are several lakes, output lines are prefixed with the lake name.

Evidence: TestAgentPushesToEachLakeAndALockedOutLakeDoesNotBlockTheOther, TestSyncPushesToEveryLakeAndKeepsGoingPastAFailure, and the status block test. go test -race ./... and the golive drills are green.
