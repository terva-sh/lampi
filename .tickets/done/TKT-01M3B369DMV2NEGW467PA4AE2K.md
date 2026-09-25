---
schema: 3
id: TKT-01M3B369DMV2NEGW467PA4AE2K
title: "Incremental sync: skip unchanged files, scan tails, debounce"
type: bug
status: done
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
claim: null
archive: null
created_at: 2026-09-25T01:34:31Z
updated_at: 2026-09-25T15:55:05Z
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

- [x] A second sync of an unchanged corpus reads no file bodies and posts no manifest
- [x] An append scans only the tail plus an overlap
- [x] Syncs are debounced
- [x] An unchanged manifest does not re-normalize on the lake

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

## Notes

**agent:claude-code/eh1m** at 2026-09-25T15:54:36Z

### Branches

The agent half is claude/elegant-feynman-eh1mdd-incremental. It was built on the resilience part-2 branch and then merged with main after #76 landed, so it targets main. The lake half is claude/elegant-feynman-eh1mdd-incremental-2. It is based on main because it does not depend on the agent. Together the two are over 1500 changed lines outside tests (1402 agent, about 350 lake). Each branch passes CI on its own.

### Measurements

- Unchanged second sync of 120 Claude sessions of 1 MiB (BenchmarkSyncUnchanged). The base took 3.0s, allocated 1038 MiB, and posted 120 manifests. With the agent's memo it takes 7.7ms, allocates 0.6 MiB, and posts 0 manifests. Without a memo (one-shot sync) it takes 118ms, allocates 13 MiB, and posts 0.
- Ruleset over 32 MiB (BenchmarkScan32MiB). Transcript-like text went from 62 to 133 MB/s. Text with no rule literal went from 161 to 1086 MB/s. The prefilter is one table lookup per pair of bytes, replacing 26 bytes.Index passes. A whole-buffer "no literal" fast path would not help on transcripts, because `sk-`, `-----`, and `secret` show up in ordinary text. That one pass is the fast path.
- Unchanged re-post of a 41 MiB chunked transcript on the lake, including any re-projection it started (BenchmarkRepostUnchangedChunked). It went from 1.05s and 762 MiB to 32ms and 0.1 MiB. With short lines, where projection costs more, main took 10.9s and 4.3 GiB.

### Design notes

- The stat check lives in a per-process memo (upload.Memo), not in the watermark row. A reader needs the session id and cwd to group a file, and the watermark does not hold those. The memo keys on path, size, mtime, and inode, and it also covers refused sessions. The session-level skip then compares the reader's digest with the watermark and checks for outbox rows. The agent keeps one memo for its life. Its first pass, and one every 6 hours, hashes every file, which catches a same-size, same-mtime rewrite. One-shot sync has no memo: it hashes every file but does not scan or post an unchanged one.
- The clean-scan offset is the watermark itself. The watermark now records the ruleset and hit count of bytes [0:Size], and SHA256 is the prefix hash. The tail scan runs only when the file still starts with those bytes and they scanned clean. ScanAppended starts 64 KiB before the boundary. A PEM BEGIN line matches on its own, so a clean prefix cannot hold an open block, and only a BEGIN line cut by the boundary is open. The unbounded spans are the AWS secret's whitespace and quote runs and the Slack webhook's slash and upper-case runs. For those the window walks back over the run and the literal before it, twice. Tests plant a key across the boundary, a 13 KiB escaped PEM block whose BEGIN the boundary cuts, and runs of 3x Overlap.
- A session that is skipped does not refresh its project fields on the lake. A HEAD that moved in the checkout reaches the lake with the next change to the session.
- The walk now stats a symlinked session file's target, so the memo sees the target grow.
- ProjectAt is cached per cwd. The cache is checked against the size and mtime of HEAD, the ref HEAD names, packed-refs, config, commondir, and shallow. A git root miss is cached for an hour.
- Debounce: 5s of quiet, at most 30s after the first event, set by config.json agent.debounce and agent.debounce_max. SIGUSR1 and the start pass do not wait. A debounce that fires during a retry wait does not start a push.
- Published examples are compared exactly against the key material, the regexp group `key` or the whole match, with slash runs collapsed. A JSON-escaped example is now also recognised.
- On the lake, resolve no longer computes relations, because Ingest recomputes them inside its transaction from the CAS. Both compare streams (catalog.Relate). IngestChanged reports whether a projection input changed: a new session, a recorded artifact, a head move, a newly learned project id, or an earlier normalize_error. The handler enqueues normalize only then. At most 4 blob PUTs and manifest posts run at once. A waiter gets its deadlines reset when it gets a slot, and gets 503 with Retry-After if its budget runs out first.
- Coordinator addition: last_attempt.json records the skip count and the first 5 skipped lines (one line each, cut at 200 bytes). status prints them under last_skipped. A pass that stops at hello still records what discovery skipped.

### Not done

The raati sidecar is still read on every pass to find its session. The Cursor readers take no memo; they rebuild an export each pass, but an unchanged export still posts nothing.

## Summary

The work is on two branches. claude/elegant-feynman-eh1mdd-incremental is the agent half and targets main. claude/elegant-feynman-eh1mdd-incremental-2 is the lake half, based on main. AC 4 lands with the lake branch.

- The agent keeps a memo of each file's digest and session by size, mtime, and inode. A session whose files all match their watermarks, with no outbox row, is not read, scanned, or posted. The first pass and one every 6 hours hash every file. One-shot sync hashes every file but does not scan or post an unchanged one. An unchanged sync of 120 x 1 MiB went from 3.0s and 1 GiB allocated to 7.7ms and 0.6 MiB.
- The watermark records the ruleset and hits of its bytes. An append to bytes that scanned clean scans the tail from 64 KiB before it (redact.ScanAppended), walking back over runs a rule repeats.
- Watch-driven syncs wait for 5s of quiet, at most 30s. SIGUSR1 and the start pass do not wait.
- The rule literals are found in one pass: 32 MiB of transcript scans at 133 MB/s, up from 62. Published examples match only exactly.
- ProjectAt is cached per cwd, and a git root miss for an hour.
- last_attempt.json and status name the files the last pass skipped.
- The lake compares prefixes as streams, skips normalize for a post that changed nothing, and runs at most 4 PUTs and manifest posts at once. An unchanged re-post of a 41 MiB chunked file went from 1.05s and 762 MiB to 32ms and 0.1 MiB.
