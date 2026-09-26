---
schema: 3
id: TKT-01M3FP115FH6DV3V5WGN3XHPJK
title: "Agent: standalone mode with no lake, and lake reload on SIGHUP"
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
  - TKT-01M3FHHBPHPZHBTJT794N8HCZW
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-26T20:20:39Z
updated_at: 2026-09-26T21:58:03Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/e4a47e8c
  name: ""
extensions: {}
---

## Description

Part of the agent onboarding epic. Let the agent run with no lake, and change its set of lakes without a restart.

- With no lake configured, the agent starts, discovers and watches, uploads nothing, and says so once. This is the standalone mode in the epic. Today an empty config falls back to the loopback default, so the no-lake state must be explicit: a legacy config with no `server` keeps that fallback, and a config with an empty `lakes` map and no `server` means no lake.
- On Unix the agent reloads its lake set on SIGHUP, without dropping in-flight work. `register` and `lakes remove` send the signal through `agent.pid`, the way the hook sends SIGUSR1. Other config still reloads on restart only.
- On Windows there is no reload. The owner decided on 2026-09-27 that a restart is required there, and the commands say so.

## Acceptance criteria

- [x] With no lake configured, the agent watches, uploads nothing and says so once
- [x] On Unix, SIGHUP reloads the lake set without dropping in-flight work
- [x] On Windows the commands that change lakes say a restart is required

## Implementation plan

Config: a present, empty lakes map with no top-level server/token_file/LAMPI_SERVER resolves to no lake; a missing map keeps the loopback default; --server/--token-file with no lake is an error (a flag overrides a lake, never adds one). Agent: pid and state come from config.StateDir, not the first lake; with no lake it prints one standalone line and growth wakes nothing. Runners move into a lakeSet (map by name, per-runner cancel and done). SIGHUP (Unix, watchReload beside watchKick) re-resolves config.json with the start flags and the start harness roots, stops gone and changed lakes, waits for their drains, then starts new and changed lakes with a wake; unchanged runners keep backoff and memo and get their label set in place. A config that fails to resolve leaves the set alone. reloadAgent(state) is the helper register and lakes remove will call: on Unix it probes the agent.pid flock and sends SIGHUP only when the lock is held; on Windows it says a restart is required. sync fails with no lake; status prints lakes: none configured.

## Notes

**agent:claude-code/e4a47e8c** at 2026-09-26T21:24:33Z

Decisions and what they rule out:

- Empty map versus missing map. Rejected: a separate `"standalone": true` flag, because it can contradict a lakes map. Rejected: treating any zero-lake config as standalone, because it would silently stop every existing machine whose config.json omits `server` and relies on the loopback default. The cost is that a writer of config.json has to keep an empty map as `{}`. With `omitempty`, marshalling File would drop it and turn the machine back into a loopback client. Nothing marshals File today; register (TKT-01M3FHHBR) must not use `omitempty` for this field when it writes.
- A changed lake is stopped and restarted rather than reconfigured in place. An in-place swap would need the runner to re-read options between pushes and a lock around every use of the options. Stopping first also means that two runners never share one lake's outbox. The old runner is waited for before the new one opens the directory.
- Unchanged lakes are kept. The label is swapped through an atomic pointer, so going from one lake to two does not restart the first lake or rehash every file.
- reloadAgent checks the flock, not the pid. A pid left by a crash can name an unrelated process, and SIGHUP's default action would kill it. The shared-lock check has a window of microseconds in which an agent starting at that moment would see the lock held and exit, naming a pid. That is accepted.
- Rejected: reloading harness roots and the debounce as well. The watchers and the debouncer are built once at start, and the ticket scopes reload to the lake set.

Evidence:

- TestAgentWithNoLakeWatchesAndUploadsNothing.
- TestSyncAndStatusWithNoLake.
- TestAgentSIGHUPReloadsTheLakes: add from standalone, unchanged, restart on new rules with drain, bad config leaves lakes alone, remove to standalone.
- TestReloadAgentSignalsOnlyAHeldPIDFile.
- TestEmptyLakesMapIsNoLake.
- `go vet` on GOOS=windows.
- Full `-race` suite and the golive drills are green.

AC3 is unticked. The commands that change lakes, register and lakes remove, arrive in TKT-01M3FHHBR. reloadAgent returns the Windows restart line they must print, and it is tested only by `go vet` on Windows. TKT-01M3FHHBR has to call it after it writes config.json.

**agent:claude-code/e4a47e8c** at 2026-09-26T21:57:53Z

Supersedes the AC3 part of the first note. register and lakes remove (TKT-01M3FHHBR) now print reloadAgent(state). On Windows that is 'restart the agent … it reads them only at start on this platform'. Evidence is the code path plus GOOS=windows vet; no test ran on Windows.

## Summary

An empty lakes map is standalone: the agent watches, uploads nothing and says so once; status and sync say no lake is configured. On Unix SIGHUP reloads the lake set, draining lakes that go or change and keeping unchanged ones running; register and lakes remove signal a running agent through agent.pid, and on Windows say a restart is required.
