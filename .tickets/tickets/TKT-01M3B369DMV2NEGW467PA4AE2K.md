---
schema: 3
id: TKT-01M3B369DMV2NEGW467PA4AE2K
title: "Incremental sync: skip unchanged files, scan tails, debounce"
type: bug
status: ready
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
