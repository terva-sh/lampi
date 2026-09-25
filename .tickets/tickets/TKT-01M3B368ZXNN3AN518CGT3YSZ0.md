---
schema: 3
id: TKT-01M3B368ZXNN3AN518CGT3YSZ0
title: "CAS: fsync, repair damaged objects, lock after the read"
type: bug
status: ready
status_reason: null
priority: high
due_on: null
labels:
  - area/cas
assignees: []
milestone: null
parent: TKT-01M3B35JS8J2F83FG4J7ZC0199
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-25T01:34:30Z
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

The CAS is not durable across a crash, and one slow upload stalls every other write.

### Findings

- No fsync. Proven. `internal/cas/cas.go:82-142` (`putLocked`), `internal/cas/resume.go:120-252` (`installCoveredLocked`, `Concat`, `commitFileLocked`) and `internal/cas/logical.go:150-187` (`writeLogical`) rename a temp file into place without syncing it or the directory. The catalog runs `synchronous=FULL`, so the manifest ACK can be durable while the blob it names is not. After a power loss on ext4 a new object can come back zero length.
- A damaged object is never repaired. Proven. `Has` (`cas.go:55`) checks existence only, and `Put` returns `exists` without comparing. A re-PUT of the right bytes leaves the bad file. `blobs/check` never reports it missing. Every later append on that session becomes `divergent_copy`, and any other session naming the digest gets a permanent 400.
- The lock is held across the network read. Proven. `Store.Put` (`cas.go:77-79`) takes the store-wide `s.mu` and then `io.Copy`s the request body. Caddy streams bodies, so a laptop on a slow link holds the lock for up to `ReadTimeout`. Every other PUT, and every tail assembly (`api/merge.go:181`), waits behind it. A 9-byte PUT took 1.8s behind a trickled body in the test. `PutRange` already reads before it locks.

### Approach

Hash into the temp file with no lock held, `Sync()` it, then take `s.mu` for the stat and rename only. Fsync the parent directory after each rename, and after `MkdirAll` creates a shard directory. When the final path exists with a different size or hash, replace it with the verified temp file rather than returning `exists`.

## Acceptance criteria

- [ ] A put, concat, range install, and logical write fsync the file and its directory before the ACK
- [ ] A re-put of correct bytes over a truncated or zero-filled object repairs it
- [ ] A PUT is not blocked by another client's slow body; a test proves it
- [ ] go test -race ./... is green
