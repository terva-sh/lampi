---
schema: 3
id: TKT-01M4ET6TFCKN6H6CZRZNAPJA63
title: Diagnose failed Operations search-index compaction
type: bug
status: in-progress
status_reason: null
priority: high
due_on: null
labels:
  - area/search
  - area/ops
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim:
  actor: agent:codex/t3code-8afe4a1e
  branch: fix/operations-compaction
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-8afe4a1e
  commit: 0616211805ac616729b3a376ec62a1093a5f5da1
  session: null
  claimed_at: 2026-10-08T22:30:47Z
  expires_at: null
archive: null
created_at: 2026-10-08T22:30:11Z
updated_at: 2026-10-08T23:19:18Z
created_by:
  id: agent:codex/t3code-8afe4a1e
  name: ""
updated_by:
  id: agent:codex/t3code-8afe4a1e
  name: ""
extensions: {}
---

## Description

The dogfooding dashboard accepted Compact search index but finished with: Maintenance did not finish. Check the lake logs before retrying. Lake and capture remain active. Obtain the underlying maintenance log, reproduce the failure safely, and fix the demonstrated cause while preserving searchable data and access controls.

## Acceptance criteria

- [x] The cause is identified from the maintenance error and reproduced safely.
- [x] The fix handles the demonstrated failure and has regression coverage.
- [ ] Compaction succeeds on dogfooding with search and capture healthy.

## Implementation plan

Continue a started forced merge with positive budgets instead of repeatedly restarting it as new uploads arrive. Keep new deletion requests pending until that merge finishes; resume pending merges conservatively after restart. Detect saturated FTS segments before explicit compaction and rebuild only the derived full-text table transactionally from existing docs, then optimize/vacuum/checkpoint. Add regression tests for interrupted merging with writes, saturation recovery, search integrity, and rollback/cancellation. Validate CI and review, then deploy with backups and verify compaction, search and capture on dogfooding.

## Notes

**agent:codex/t3code-8afe4a1e** at 2026-10-08T22:30:47Z

The user reports the asynchronous failure message, so the action was accepted and m.run returned an error. Both services remain active; the filesystem has 55 GiB free. Existing web and recall tests pass. Code inspection found no demonstrated cause yet. This account cannot read the system journal and sudo -n requires a password; a filtered root journal command was requested from the user. Do not infer corruption or retry destructive cleanup from the generic UI message.

**agent:codex/t3code-8afe4a1e** at 2026-10-08T22:33:33Z

The user supplied two matching log entries: action=search, search: optimize: database or disk is full (13). This locates the failure in the optimize/vacuum stage, before post-job sampling. Root and temporary filesystems currently have 55 GiB and 5.9 GiB free, respectively; inode space is available. The service uses PrivateTmp=yes and ProtectSystem=strict, with its lake writable. A read-only aggregate diagnostic script was validated against the isolated synthetic lake, then requested for root execution to measure the live index, FTS segment counts and filesystem space in the service namespace. No live cleanup, configuration change or database repair has been attempted.

**agent:codex/t3code-8afe4a1e** at 2026-10-08T23:02:21Z

Live read-only diagnostics show a 4.54 GiB index, 776 indexed sessions, no freelist pages, and exactly 2000 distinct FTS segment IDs (1 through 2000). Filesystems and page-count limits have ample room. An isolated SQLite fixture reproduced saturation after 997 repeated negative-budget merges interleaved with inserts: merge and optimize both return SQLITE_FULL at 2000 segments, while rebuilding the FTS table from its existing external content succeeds. Repeated forced merge starts are the demonstrated source: new segments arrive between reclaim passes while the deleted flag keeps requesting negative merges.

**agent:codex/t3code-8afe4a1e** at 2026-10-08T23:07:56Z

Implemented merge continuation with positive budgets after one forced start, retaining later deletion requests for a subsequent merge and resuming conservatively after restart. Explicit compaction detects 2000 occupied segment IDs and transactionally rebuilds only FTS from docs before optimizing. Successful online compaction clears transient per-generation failures and schedules reconciliation. The synthetic saturated fixture fails ordinary optimize, and recovery preserves existing hits, passes FTS/content integrity-check, compacts to one segment, and retries a session whose insert failed at saturation. A trigger-injected rebuild failure and canceled request both leave the saturated index and old search hits intact. The earlier bounded-size regression now drains pending reclaim passes before measuring: continuing a merge may defer a new forced request by a pass. Targeted tests, vet and formatting pass; race/CI validation follows.

**agent:codex/t3code-8afe4a1e** at 2026-10-08T23:09:11Z

Correction to the previous validation note: the initial trigger-based rollback injection did not pass; a trigger on SQLite private FTS tables exposed a recursive driver rollback failure. It has been removed, with no production code using shadow-table triggers. The final test instead temporarily limits SQLite row length on its sole connection. Rebuild then fails while reading 4000-byte fixture rows, rolls back all prior FTS writes, retains 2000 segments and all existing hits, and succeeds after the limit is restored. This targeted test now passes. Merge-continuation coverage also reopens the index during an unfinished merge.

**agent:codex/t3code-8afe4a1e** at 2026-10-08T23:10:10Z

PR #207: https://git.local.sothr.com/terva-sh/lampi/pulls/207, head 4dc27d0356bfcbf8dd3ca6bca3e67fe941b338a9. Final targeted merge/recovery tests pass, including restart and limited-row rollback. Vet and formatting pass. The production lake has not been modified, and dogfooding-success criterion 3 remains unchecked. Full race tests and Forgejo review/CI are in progress. The rollout will include the already-merged pond icon and this fix, preserve a protected checkpoint, and use the existing admin compaction control for recovery.

**agent:codex/t3code-8afe4a1e** at 2026-10-08T23:10:45Z

Clean review at https://git.local.sothr.com/terva-sh/lampi/pulls/207#issuecomment-28266, run c12bba7c-7cc6-4c7c-9975-c80d08eed936; reviewed head 4dc27d0356bfcbf8dd3ca6bca3e67fe941b338a9 and base a3c25eef8e2d8479883eab88f2f43a2be7422b99. No findings. Full relevant recall/web race suites passed (250.8 s / 38.9 s); the final added rollback-limit and restart assertions also passed targeted non-race tests and are running under race separately. Forgejo full gates cover the final head. No live recovery yet; after rollout the admin should request Compact search index once and report the result.

**agent:codex/t3code-8afe4a1e** at 2026-10-08T23:19:18Z

PR #207 merged as d7dcde1ab7a337d0cd392fc1515bf9ea9095076a, now on both Forgejo and GitHub main. Final head 2623087cd0c6516cf530823ecfd530c083974fe3 passed all Forgejo gates and carried review. Final focused rollback/restart tests passed under race in 226.1 s. A built-binary synthetic rehearsal accepted one blob/manifest, backed up and fsck-verified it, and the previous dogfooding binary opened that schema-21 backup. An initial empty-lake fsck lacked a CAS directory; the seeded nonempty rehearsal matches this rollout and passed. The verified external operator bundle installs exactly merged d7dcde1 (binary SHA256 633fbc42401470a5b6fcaf5ca01f6b3ac2f3a531bce85b8d7c5adde4eb520bde), includes the merged pond icon, reserves recovery headroom, backs up the lake/config/state and original search index, verifies health/auth and public icon bytes, and resumes capture. No public release/tag, schema migration, or capture binary upgrade. Live installation requires the user terminal because sudo -n still requires a password; criterion 3 remains open until the admin compaction succeeds.
