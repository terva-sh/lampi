---
schema: 3
id: TKT-01M3B369JQHQK0ES07FEP1RX5B
title: "Operator commands: backup, purge, fsck, read-only export"
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/ops
  - area/server
  - area/auth
assignees: []
milestone: null
parent: TKT-01M3B35JS8J2F83FG4J7ZC0199
origin: null
dependencies:
  - TKT-01M3B368ZXNN3AN518CGT3YSZ0
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-25T01:34:31Z
updated_at: 2026-09-25T02:47:36Z
created_by:
  id: agent:claude-code/eh1m
  name: Claude Code cloud agent
updated_by:
  id: agent:claude-code/eh1m
  name: Claude Code cloud agent
extensions: {}
---

## Description

Operator commands the lake needs soon after go-live.

### Findings

- `export` runs a second lake. Read. `internal/cli/export.go:79` calls `api.Open`, which loads `normalize_jobs` and starts workers. The generation check and `lockSession` hold within one process, so an older generation can overwrite newer derived files. Contention can also fail a serve write transaction, because a deferred transaction's upgrade returns `SQLITE_BUSY` without the `busy_timeout` retry.
- No purge. `docs/policy.md` says bytes stay until an explicit manual purge, and there is no purge command. A secret that slips past the scan is manual SQL and file work.
- No cleanup or fsck. Stale `.put-*` temp files and abandoned `cas/partial/*` stay forever. Nothing re-hashes stored objects.
- Normalize worker. A transient catalog error drops the job from memory until the next restart: `runNormalize` returns without a requeue when `NormalizeGen` or `Session` errors (`internal/api/worker.go:151-157`, `:162-165`). A `StoreEvents` failure is retried three times and then dropped the same way (`:166-172`). A transient CAS read error becomes a permanent `normalize_error`. `s.pubs` grows by one mutex per session.
- Tokens. A token change needs a restart, which cuts uploads. A `#label` line in the token file became a working token, and in directory mode `laptop~` or `laptop.revoked` stays enrolled (`loadDeviceDir` and `loadDeviceFile`, `internal/auth/devices.go:112-182`). Proven.
- `make build` does not stamp a version; `just build` does.

### Approach

`serve backup` (`VACUUM INTO` plus the CAS copy order). `export` opens the catalog read-only without workers, or refuses while serve holds a lock file; `Ingest` uses `BEGIN IMMEDIATE`. `purge --session <uid>` removes catalog rows, derived files, and blobs no other artifact names. A start-up sweep of temp and partial files older than a day. `serve fsck` re-hashes objects. Retry transient normalize errors. Plaintext tokens must be 64 lowercase hex, `#` lines are comments kept through the rewrite, directory mode loads one suffix, and SIGHUP reloads. Stamp the version in the Makefile.

## Acceptance criteria

- [x] serve backup writes a consistent catalog copy
- [x] export against a live lake does not start workers
- [x] purge removes a session and its unreferenced blobs
- [x] fsck re-hashes objects and names bad ones
- [x] Token files accept only 64-hex tokens and # comments, and SIGHUP reloads them

## Implementation plan

The whole ticket is too big for one reviewable PR, so it is delivered as two stacked branches.

### Branch claude/elegant-feynman-eh1mdd-operator

- `internal/lakelock`: a lock file, `lake.lock`, in the data dir. It uses flock on unix, and O_EXCL plus a pid check elsewhere. `serve` holds it for its lifetime and refuses to start when another process has it.
- `export` takes the lock. When it gets the lock, nothing else writes the lake, so export keeps today's behaviour: workers drain, and a missing JSONL is projected. When serve holds the lock, export opens the catalog read-only (`api.OpenReadOnly`, with no queue and no workers). It names a session that has no derived file as not yet normalized.
- `catalog.Open` adds `_txlock=immediate`. Ingest and every other write transaction then take the write lock at BEGIN and wait on busy_timeout, instead of failing the lock upgrade. `catalog.OpenReadOnly` opens with `mode=ro`.
- `terva-lampi serve backup --out DIR [--data DIR] [--token-file PATH]`: VACUUM INTO from a read-only connection, which works while serve runs. It then copies cas/sha256 and cas/logical without temp files, and then the token file. A second run into the same directory copies only the objects it lacks.
- `terva-lampi serve fsck [--repair]`: re-hashes every object and reads every logical index. It names each bad one and exits non-zero. `--repair` takes the lock and removes the bad ones, so Has reports them missing and a client put restores them.
- serve start-up sweep: removes `.put-*`, `.logical-*`, and `.meta-*` temp files, and `cas/partial` uploads untouched for 24h.
- `writePartial` fsyncs the partial dir after the rename.
- The Makefile build stamps the version and commit, as the justfile does.
- #65 follow-ups: `normalizeQueue.push` does nothing once the queue is closed, and `fail` logs the remapped body error instead of the lake error.

### Branch claude/elegant-feynman-eh1mdd-operator-2, on top of the first

- `terva-lampi serve purge --session UID [--yes]`: dry run by default. It takes the lock. It removes the session's blobs, its logical indexes and their chunks, the tail blobs of its grown_from chain, and the chunks its last manifest named, unless another session names them. It then removes the derived files, then the catalog rows.
- Token file: a plaintext line must be 64 lowercase hex. `#` lines are comments and survive the rewrite, which keeps line order. Directory mode loads `*.token` files only. SIGHUP reloads into the same `*auth.Devices`, which is guarded by a lock, so requests in flight are not dropped. A failed reload keeps the old set.
- Normalize worker: catalog and CAS read errors are transient. The job is requeued with backoff for a bounded number of attempts. Once the attempts run out, the row stays in normalize_jobs for the next start. A transient Project error is also recorded on the session, and the next success clears it. `s.pubs` entries are refcounted and deleted when unused.

## Notes

**agent:claude-code/eh1m** at 2026-09-25T02:35:37Z

First of two stacked branches: claude/elegant-feynman-eh1mdd-operator. It holds the lake lock, read-only export, BEGIN IMMEDIATE, serve backup, serve fsck, the start-up sweep, the writePartial directory fsync, and the Makefile stamp. Purge, the token file rules with SIGHUP, and the normalize worker retry are on claude/elegant-feynman-eh1mdd-operator-2. The ticket is finished there.

### Choices

- The commands are `serve backup`, `serve fsck`, and (on the second branch) `serve purge`. They are subcommands, like `agent discover`, and they group the lake-side operator commands under the command that owns the lake.
- The lock file is `lake.lock` in the data dir. On unix, flock ends with the process, so a crash leaves nothing stale. Elsewhere it is O_EXCL, and a file whose pid is no longer running is replaced.
- export takes the lock instead of only checking it. With the lock held, nothing else can write while export runs its own workers, so its behaviour when serve is stopped is unchanged.
- `_txlock=immediate` is added to the path DSN as the smallest change. #64 replaces `catalog.Open` with a file-URI `dataSource`. When the two meet, `_txlock=immediate` belongs in that query. Expect a conflict in `catalog.Open`.
- A backup into an existing directory copies only objects it lacks, judged by size, and replaces catalog.db with a verified VACUUM INTO copy.
- fsck `--repair` takes the lock, because a repair running beside serve could remove an object a put had just fixed. Repair also hashes again under the store lock. An object that is removed is restored by the next upload of that file. A file that never changes again stays missing until an operator re-syncs it. fsck names the problem either way.

### Coordinator follow-ups from #65

- `normalizeQueue.push` does nothing after shutdown (TestQueuePushAfterShutdownIsDropped).
- `fail` logs the remapped body error, not the lake error (an assertion in TestSlowBlobBodyTimesOut).

### Expected conflicts

- docs/vps-bringup.md: #63 adds a longer Backup section. Here it is a short `## Backup` paragraph. #63 also says objects are never removed, which purge and fsck --repair now make false.
- internal/catalog/catalog.go Open: #64.
- internal/api/worker.go: #64 adds a recover. The second branch changes runNormalize.

**agent:claude-code/eh1m** at 2026-09-25T02:47:26Z

Second branch: claude/elegant-feynman-eh1mdd-operator-2, stacked on claude/elegant-feynman-eh1mdd-operator. Criteria 3 and 5 are ticked in this branch's ticket commit.

### Purge

- `serve purge --session UID [--yes]` is a dry run unless `--yes` is given, and it refuses while serve holds lake.lock. It opens the lake with `api.OpenIdle`, which starts no worker, so a queued job for the session cannot run during the purge.
- A tail blob is not recorded anywhere in the catalog, so purge recomputes it. For each path it reads the newest bytes and hashes the span between each pair of recorded sizes that are both prefixes. That is what a tail PUT was, because a tail's prev must equal the stored head's length. TestPurgeRemovesSessionAndUnsharedBlobs checks that the first tail is found this way.
- Purge does not find a chunk list or tail from an older manifest that no row records, a tail from a 409 prefix mismatch, or a blob put for a manifest that was never posted. Finding those needs a whole-lake sweep of unreferenced objects, which would also remove the Concat chunks of every other session. That is left out on purpose. The usage text and policy.md say so.

### Tokens

- The rewrite now keeps line order, blank lines, and comments. It used to sort the hashes, which would have separated a `# laptop` label from its token.
- Directory mode loads only `*.token`. The names it skips go to stderr, and an error for a directory with no token names the skipped files. README, architecture.md, and vps-bringup.md say that existing directories must rename device files to `.token` before upgrading.
- The unit file is not changed. `systemctl kill -s HUP` is documented instead, because #63 rewrites the unit.

### Normalize worker

- There are five attempts, and the wait doubles from 200ms. A job that runs out of attempts stays in normalize_jobs for the next start. A CAS read that is still failing at the end is also recorded as normalize_error so export names it, and its row is kept, so the next start's success clears the error. A catalog error cannot be recorded, so it is only logged.
- `s.pubs` entries are refcounted and deleted at zero.

## Summary

The work landed on two stacked branches.

### claude/elegant-feynman-eh1mdd-operator

- serve holds lake.lock (`internal/lakelock`) and refuses to start a second time on the same data dir.
- export takes the same lock. When serve holds it, export reads the catalog read-only, starts no worker, and names a session with no derived file as not yet normalized.
- Catalog transactions use BEGIN IMMEDIATE (`_txlock=immediate`).
- `serve backup` writes a VACUUM INTO copy of the catalog, then the CAS without temp files or partial uploads, then the token file. A second run copies only new objects.
- `serve fsck [--repair]` re-hashes the objects, reads the logical indexes, names the bad ones, and exits non-zero.
- At start, serve removes temp files and partial uploads older than 24h.
- writePartial fsyncs its directory.
- The Makefile stamps the version.
- Two follow-ups from #65: a queue push after shutdown is dropped, and the access log records the remapped body error.

### claude/elegant-feynman-eh1mdd-operator-2

- `serve purge --session UID [--yes]` is a dry run unless `--yes` is given. It removes the session and every blob no other session names: logical indexes and their chunks, and tail blobs, which it recomputes.
- A token file accepts only 64-hex tokens and `#` comments, and the comments are kept through the rewrite. A token directory loads `*.token` files only. SIGHUP reloads the tokens in place.
- The normalize worker retries transient catalog and CAS errors with backoff. When the attempts run out it keeps the job row. It also drops session locks nobody holds.
