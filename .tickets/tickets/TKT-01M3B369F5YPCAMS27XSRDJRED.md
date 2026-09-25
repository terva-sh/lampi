---
schema: 3
id: TKT-01M3B369F5YPCAMS27XSRDJRED
title: "Agent resilience: per-file errors, watcher fallback, one config"
type: bug
status: ready
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
claim: null
archive: null
created_at: 2026-09-25T01:34:31Z
updated_at: 2026-09-25T01:38:33Z
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

- [ ] One unreadable or oversized file is logged and skipped and the rest upload
- [ ] The agent runs with no terva home and falls back to polling on watcher errors
- [ ] sync, status, and agent resolve the server and token the same way, and status prints the source
- [ ] Refusals and quarantine records are logged on change only, and quarantine can be listed and acknowledged
