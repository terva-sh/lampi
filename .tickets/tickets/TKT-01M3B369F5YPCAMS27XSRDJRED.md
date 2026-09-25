---
schema: 3
id: TKT-01M3B369F5YPCAMS27XSRDJRED
title: "Agent resilience: per-file errors, watcher fallback, one config"
type: bug
status: in-progress
status_reason: null
priority: high
due_on: null
labels:
  - area/agent
assignees: []
milestone: null
parent: TKT-01M3B35JS8J2F83FG4J7ZC0199
origin: null
dependencies: []
blocks_on: none
references: []
claim:
  actor: agent:claude-code/eh1m
  branch: claude/elegant-feynman-eh1mdd-resilience
  worktree: /home/user/lampi/.claude/worktrees/agent-a74fa3c5712698894
  commit: ae4e6dcaa2db3559ec4d24c9ff889888dd444fca
  session: null
  claimed_at: 2026-09-25T14:40:33Z
  expires_at: null
archive: null
created_at: 2026-09-25T01:34:31Z
updated_at: 2026-09-25T14:56:54Z
created_by:
  id: agent:claude-code/eh1m
  name: Claude Code cloud agent
updated_by:
  id: agent:claude-code/eh1m
  name: Claude Code cloud agent
extensions: {}
---

## Description

Single failures stop the agent for every harness, and the operator cannot see why.

### Findings

- One bad file stops every harness. Proven. A Claude or Codex line over 8 MiB before `cwd` or `sessionId` (`adapter/claude/claude.go:181`, `codex/codex.go:184`), or a terva first line over 1 MiB (`terva.go:284`), fails the whole run. By reading, so does an unreadable directory or a file removed mid-walk. The error is not `Rejected`, so it retries every 2s.
- No terva home, no agent. Proven. `internal/cli/peers.go:36,216` marks the terva home required. `internal/watch/watch.go:202` fails on the missing root and the agent exits with `watch: no such file or directory`, then crash-loops under systemd. An `ENOSPC` inotify limit or an fsnotify error (`internal/watch/watch.go:206,340-346,374-376`) also exits. On darwin kqueue holds one fd per watched file.
- Server URL disagrees between commands. Proven. The systemd unit always sets `LAMPI_SERVER` to loopback and the launchd plist always passes `--server`, so a `server` in `config.json` loses. `sync.go:119`, `status.go:92`, and `agent.go:462` ignore `LAMPI_SERVER`, so with the VPS URL in `agent.env`, `status` still probes loopback.
- Logs flood, and quarantine is a dead end. Proven. `agent.go:278` prints every refusal on every sync. `quarantine.jsonl` gains a record per sync (`redact/quarantine.go:31-53`). A hit blocks a session for good; the only ways out are editing the transcript or the global `upload_hits`. `status` does not show the last error.
- Smaller. A symlinked harness root finds 0 files (`internal/adapter/walk.go:33`, `filepath.WalkDir` on the base). `agent.pid` is overwritten with no liveness check, so two agents can run. `machine.json` is written non-atomically (`config/config.go:192`). The outbox is never read back (`Pending` is test-only).

### Approach

Isolate failures per file and session, treat an overlong line as no identity, and skip `ENOENT` and `EACCES` in the walk. Watch the terva home like the other optional roots, fall back to polling on watcher errors, name the path, and poll by default on darwin. One resolver for server and token (flag, env, config, default) shared by every command; drop the fixed values from the units; `status` prints the winning source. Log a refusal or quarantine record only on change. Add `terva-lampi quarantine list` and `quarantine allow <relpath|sha>`. Persist and show `last_error` and `last_attempt`. `EvalSymlinks` the harness root, lock the pidfile, write `machine.json` by rename, and either read the outbox or remove it.

## Acceptance criteria

- [x] One unreadable or oversized file is logged and skipped and the rest upload
- [x] The agent runs with no terva home and falls back to polling on watcher errors
- [ ] sync, status, and agent resolve the server and token the same way, and status prints the source
- [ ] Refusals and quarantine records are logged on change only, and quarantine can be listed and acknowledged

## Implementation plan

Two stacked branches, as the operator ticket did: claude/elegant-feynman-eh1mdd-resilience carries items 1, 2 and 5; claude/elegant-feynman-eh1mdd-resilience-2 carries items 3 and 4 on top of it.

### Branch 1: isolation, watcher, small fixes

- adapter.Bundle gains Skipped, a list of errors that each name one file. The adapter and discover walks skip an entry that vanished (ENOENT) or cannot be read (EACCES) below the base and report it; Walk keeps its signature for Discover.
- A shared adapter.ScanLines reads with bufio.Reader and discards a line past the cap instead of failing. Claude and Codex keep looking for identity past it. When identity is still incomplete after an overlong line, the file is skipped with a reason, because its id may have been on that line and a relpath id would split the session. A terva first line past 1 MiB is the same skip.
- Claude, Codex, OpenCode and terva skip a file whose identity read or hash fails and keep the rest. upload.bundlesFor isolates each harness: a failed Manifests is one skip line and the other harnesses still upload. readArtifacts treats a file gone or unreadable since the digest like errFileChanged, with a skip line. Result.Skipped is printed on stderr by sync and the agent.
- watch: a missing root polls until it exists, then tries fsnotify. An Add failure (ENOSPC included) or a fsnotify error drops that watcher to polling and reports the path through OnFallback instead of returning. darwin polls by default; LAMPI_WATCH=fsnotify or poll overrides it. The terva home is no longer required to exist.
- Harness roots resolve through EvalSymlinks before the walk. agent.pid is created with O_EXCL; a stale file whose pid is not alive is replaced, a live one stops the second agent with a message naming the pid. machine.json is written to a temp file and renamed. The outbox gets its decision in a note.

### Branch 2: one config, quiet logs, quarantine

- One resolver (flag, env, config.json, default) returns value and source for server and token file. agent, sync, status, agent config, login and the conflicts token use it. status and agent config print server_source and token_file_source. The systemd unit drops its Environment= pins and the plist drops --server.
- The agent logs a refusal line only when it was not in the previous pass. AppendQuarantine skips a record whose relpath, digest and rules match one already in the log.
- quarantine list prints the records. quarantine allow <relpath|sha256> writes the digest to an allow file in the state directory; scanSession stamps override for that exact digest only. A manifest hit cannot be allowed.
- Sync records the last attempt and its error in the state directory; status prints last_attempt and last_error.

## Notes

**agent:claude-code/eh1m** at 2026-09-25T14:56:54Z

Branch claude/elegant-feynman-eh1mdd-resilience carries items 1, 2 and 5. Decisions a reviewer should know:

### Overlong lines
The reader does not raise the cap. adapter.ScanLines reads with bufio.Reader and reads past a line over the cap without holding it. Claude and Codex keep looking for identity after it, so a big tool result on line 3 no longer costs the file. A file whose id or cwd is still missing after an overlong line is skipped with a LongLineError, because the id may have been on that line and a relpath id would split the session. A terva first line over 1 MiB is the same skip.

### Isolation boundaries
The terva, Claude, Codex and OpenCode loops skip one file. The Cursor IDE and Cursor CLI loops are the Cursor ticket's files, so they were left alone; upload.bundlesFor isolates each harness, so a Cursor failure is one skip line and every other harness still uploads. The shared walk (discover.WalkFiles) covers Cursor discovery too.

### Missing homes
Every enabled home is now watched, not only terva's. One that does not exist is polled until it appears, then fsnotify is tried. Before, a Claude or Codex install after the agent started needed a restart.

### darwin
No override existed, so LAMPI_WATCH=poll|fsnotify was added. Unset polls on darwin and prefers fsnotify elsewhere.

### Outbox
Documented rather than replayed. A row holds digests, not bytes, and the file may have changed since, so reading Pending on drain could not upload anything the regular pass would not. The drain is already a full pass. The package doc and docs/architecture.md now say the outbox is the durable count status reports. Pending stays for inspection and tests.

### agent.pid
flock on Unix (with a same-inode check after the lock), O_EXCL plus a process check elsewhere. The file keeps the pid, so the hook is unchanged.

### Not covered by a failing-first test
machine.json by rename has no test that fails before the change; atomicity is not observable from a unit test. The EACCES walk test skips as root; it was run and passed as uid 65534 with setpriv. The removed-mid-walk and fn-permission tests cover the same code path for any user.
