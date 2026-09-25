---
schema: 3
id: TKT-01M3B368ZXNN3AN518CGT3YSZ0
title: "CAS: fsync, repair damaged objects, lock after the read"
type: bug
status: done
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
updated_at: 2026-09-25T01:52:59Z
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

- [x] A put, concat, range install, and logical write fsync the file and its directory before the ACK
- [x] A re-put of correct bytes over a truncated or zero-filled object repairs it
- [x] A PUT is not blocked by another client's slow body; a test proves it
- [x] go test -race ./... is green

## Implementation plan

### Durability

Add `syncDir` (a no-op on Windows, where a directory handle cannot be flushed) and `mkdirSynced`, which fsyncs the parent of each directory it had to create. Every install path syncs the file before the rename and the directory after it: `Put`, `Concat` and a covered `PutRange` through `commitFileLocked`, and `writeLogical` for a logical index. `PutRange` also syncs the partial data before `meta.json` claims its spans, so a crash cannot leave a span recorded over bytes that never reached the disk.

### Repair

`intactLocked(digest, size)` stats the object, compares the size, then hashes it. The size check is free and bounds the hash to the size the caller already knows (at most the blob cap). `commitFileLocked` renames the verified temp over an object that is not intact instead of returning `exists`. The early `Has` short circuits in `Concat` and `PutRange` become `intactLocked`, so they fall through to a rebuild. `BindLogical` removes a damaged installed object once the chunks verify, so `Open` serves the logical index instead.

### Lock scope

`Put` creates the shard directory, streams, hashes, and syncs the temp file with no lock held, then takes `s.mu` for the verify and rename only. A test holds one `Put` on a blocking reader and requires a second `Put` to finish.

### Not changed

`Has` still checks existence only, so `blobs/check` reports a damaged object as present. A verified check would hash every object on every check; the repair lands on the next re-PUT, a tail assembly (`api/merge.go` calls `Put`), or a concat.

## Notes

**agent:claude-code/eh1m** at 2026-09-25T01:46:46Z

Departures from the proposed approach, and what is left for others.

### Verify cost

The early checks in `Concat`, `PutRange` and `BindLogical` hash the stored object at its stored size (`intactLocked(digest, -1)`), so a wrong `total` or `lengths` on an intact digest keeps its old `exists` answer. `commitFileLocked` passes the verified temp's size, so a size mismatch replaces without hashing. Every object was installed under the blob cap, so the hash is bounded.

### Test seam

`syncDir` is a package variable so `durable_test.go` can record which directories are flushed and that the object is already in place when its directory is. File `Sync()` calls are not observable without a filesystem fault injector; they sit next to each rename.

### Not in this ticket (internal/api)

`Has` still checks existence only. `api/server.go` short-circuits a chunk-list PUT and `blobs/check` on `Has`, and `api/merge.go` checks chunks and tails with `Has`, so a damaged object is still reported present there. It is repaired when a PUT of its bytes reaches the store, which includes the tail assembly `Put` in `api/merge.go` and any `Concat`. A verified `blobs/check` would hash every present object on every check.

**agent:claude-code/eh1m** at 2026-09-25T01:52:59Z

Has now reports an empty object under any digest but the empty one as missing. A crash before the data reached disk leaves exactly that, so blobs/check asks the client again and the put repairs it. Damage of the right size still reads present until a put of those bytes or a later fsck.

## Summary

Landed on `claude/elegant-feynman-eh1mdd-cas`.

`Put`, `Concat`, a covered `PutRange` and `writeLogical` fsync the file before the rename and the directory after it. `mkdirSynced` flushes a new shard directory into its parent. `syncDir` does nothing on Windows. A range's data is synced before `meta.json` records the span.

`commitFileLocked` replaces an object whose size or sha256 is wrong instead of returning `exists`. The early checks in `Concat` and `PutRange` verify too, so they rebuild. `BindLogical` removes a damaged object so reads use the chunks.

`Put` streams and hashes with no lock held and takes `s.mu` only for the verify and rename. `TestSlowBodyDoesNotBlockOtherPut` holds one `Put` on a blocked reader and needs a second to finish; it fails in 5s on the old code.

docs/protocol.md and docs/architecture.md describe the repair and the fsync. `Has` and the api callers that trust it are unchanged; see the note.
