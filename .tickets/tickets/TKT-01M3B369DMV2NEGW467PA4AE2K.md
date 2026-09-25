---
schema: 3
id: TKT-01M3B369DMV2NEGW467PA4AE2K
title: "Incremental sync: skip unchanged files, scan tails, debounce"
type: bug
status: in-progress
status_reason: null
priority: high
due_on: null
labels:
  - area/agent
  - area/server
assignees: []
milestone: null
parent: TKT-01M3B35JS8J2F83FG4J7ZC0199
origin: null
dependencies:
  - TKT-01M3B369A74CEB0MPSVH2MAHQV
  - TKT-01M3B369BNP6HM2AM4B78V99F2
blocks_on: none
references: []
claim:
  actor: agent:claude-code/eh1m
  branch: claude/elegant-feynman-eh1mdd-incremental
  worktree: /home/user/lampi/.claude/worktrees/agent-aa11f1af8919da581
  commit: 6877236cdbc2cd55fed4493a57aa077249b4af31
  session: null
  claimed_at: 2026-09-25T15:17:00Z
  expires_at: null
archive: null
created_at: 2026-09-25T01:34:31Z
updated_at: 2026-09-25T15:17:16Z
created_by:
  id: agent:claude-code/eh1m
  name: Claude Code cloud agent
updated_by:
  id: agent:claude-code/eh1m
  name: Claude Code cloud agent
extensions: {}
---

## Description

Every sync re-reads, re-scans, and re-posts the whole allowlisted corpus, and the watcher starts a sync on every write.

### Findings

- Proven with 100 Claude sessions of 1 MiB. An unchanged sync took 35s and posted 100 manifests, and the heap grew by the corpus size. 88% of CPU was `redact.Scan`, at about 3 MB/s.
- `internal/upload/prepare.go:130-241` reads and scans every file, keeps each body, and puts unchanged files back into the work list. `internal/cli/agent.go:199-202` runs a full `upload.Sync` per fsnotify write. The adapters hash every file and call `ProjectAt` for every session, refused ones included. `ProjectAt` spawns `git` whenever `rootLoose` misses, which is any packed repository (`internal/adapter/git.go:237-240`, `rootGit` at `:364`).
- On the lake, `api/merge.go:43-50` re-reads every current blob of the session per post, and an unchanged post bumps the generation and re-projects the session. An unchanged re-post of a 41 MiB chunked file allocated 207 MiB and re-normalized for 13s.

### Approach

Skip a file whose size and mtime match its watermark before reading it. Scan the new tail plus an overlap, with the clean-scan offset kept against the prefix hash. Post no manifest when nothing changed. Cache `ProjectAt` per cwd for the process. Debounce syncs to 5 to 30s. A literal prefilter in front of the rule set. On the lake, skip the enqueue when every decision is unchanged, compare prefixes by streaming a hash, and bound concurrent PUTs and manifests.

## Acceptance criteria

- [ ] A second sync of an unchanged corpus reads no file bodies and posts no manifest
- [ ] An append scans only the tail plus an overlap
- [ ] Syncs are debounced
- [ ] An unchanged manifest does not re-normalize on the lake

## Implementation plan

Split in two branches. claude/elegant-feynman-eh1mdd-incremental (on the resilience part-2 branch) is the agent. claude/elegant-feynman-eh1mdd-incremental-2 (on main, since the lake has no dependency on the agent) is the lake.

### Re-verified on the base

- An unchanged second sync of 120 Claude sessions of 1 MiB takes about 3.0s and allocates about 1 GiB, and posts 120 manifests. Most of the CPU is `redact.find` (bytes.Index on the rule literals, then regexp).
- `prepare` reads and scans every file. `stamp` keeps a KindUnchanged artifact in the manifest, so every session is posted again.
- The adapters hash every file and read its head for the identity before prepare sees it.
- `ProjectAt` no longer runs git. `ResolveRoot` does, for an admitted session whose root the in-process reader missed, and a miss is not cached.
- `api/merge.go resolve` reads every current blob of the session into memory, and an unchanged post still enqueues normalize.

### Agent

1. A per-process memo of what each reader took from a file (digest and identity), keyed by path, size, mtime, and inode. A file whose stat matches is not opened. The first pass of a process, and one every 6 hours, ignores the memo and hashes every file; that catches a rewrite in place with the same size and mtime. The agent keeps the memo across passes. One-shot sync hashes, but does not scan or post.
2. prepare skips a session whose every artifact matches its watermark digest and size and has no outbox row, before ResolveRoot and before any read. No manifest.
3. The watermark records the ruleset and the hit count of the bytes it covers. When the prefix still hashes to the watermark and that scan was clean under this ruleset, only the tail is scanned, from an overlap before the boundary. The overlap is 64 KiB and walks back over a run a rule can repeat without limit (whitespace and quotes for the AWS secret, slashes and upper-case for the Slack webhook). A PEM BEGIN line matches on its own, so a clean prefix cannot hold an open block.
4. ProjectAt is cached per cwd, checked against the size and mtime of HEAD, the ref HEAD names, packed-refs, config, and shallow. A ResolveRoot miss is cached for an hour.
5. Agent syncs from watch events are debounced: 5s of quiet, at most 30s after the first event. config.json agent.debounce and agent.debounce_max. SIGUSR1 and the start pass stay immediate. The retry wait is unchanged.
6. Measure a prefilter in front of the rule set on 32 MiB and keep it only if it helps.
7. The published-example check compares the key material exactly.
8. last_attempt.json records the last pass's skipped lines, and status prints them.

### Lake

- Compare the prefix by streaming a hash of the stored head and read only what a decision needs.
- Skip the normalize enqueue when every decision is unchanged or stale.
- Bound concurrent blob PUTs and manifest posts with a semaphore of 4. A waiter holds until a slot frees or its deadline ends.
