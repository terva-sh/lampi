---
schema: 3
id: TKT-01M4F4SE0VVD1HMZ7SSJCA290E
title: Search readers fail to connect during index maintenance
type: bug
status: in-progress
status_reason: null
priority: high
due_on: null
labels:
  - area/search
  - area/server
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim:
  actor: agent:codex/t3code-8afe4a1e
  branch: fix/search-connection-during-maintenance
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-8afe4a1e
  commit: 10d86676f16f3879162102ac5a7c4d03a354a9af
  session: null
  claimed_at: 2026-10-09T01:35:31Z
  expires_at: null
archive: null
created_at: 2026-10-09T01:35:06Z
updated_at: 2026-10-09T01:43:58Z
created_by:
  id: agent:codex/t3code-8afe4a1e
  name: ""
updated_by:
  id: agent:codex/t3code-8afe4a1e
  name: ""
extensions: {}
---

## Description

After the d7dcde1 dogfooding upgrade, Operations successfully compacted the search index from 3252.5 MiB to 3138.2 MiB (114.3 MiB reclaimed). Search requests for Corruption also returned read_failed. The operator's filtered journal shows HTTP 500 after approximately five seconds with database is locked (5) (SQLITE_BUSY), rather than evidence of index corruption. Lake health and lake/capture services remain active.

Source inspection and a standalone synthetic SQLite probe reproduce a likely cause: indexDSN configures auto_vacuum(INCREMENTAL) on every pooled connection. Opening a new connection while a WAL writer is active fails with SQLITE_BUSY even though an existing reader reads the committed snapshot. This can happen during compaction or ordinary indexing. The current online optimize test verifies reads after the job, rather than a new pooled reader during a write.

Move the file's persistent auto-vacuum setting to initialization before schema creation, preserve it for current indexes, and keep per-connection settings needed for WAL reads. Add a deterministic regression that opens a fresh numbered-search reader during an uncommitted index write and confirms the committed results and pages remain readable. Verify fresh/reopened indexes retain incremental vacuum and the existing compaction regression suite still passes. No catalog, blob, protocol or search schema change is planned.

This is separate from TKT-01M4F4KWYXX399JBZ9GSKFVMKQ — CAS: compact stored blobs while the lake stays online (draft assessment). The original FTS recovery ticket, TKT-01M4ET6TFCKN6H6CZRZNAPJA63 — Diagnose failed Operations search-index compaction, has a successful live compaction result but its healthy-search criterion remains pending until this problem is resolved.

## Acceptance criteria

- [x] A regression reproduces fresh pooled search readers failing while a WAL writer is active.
- [x] Numbered searches read committed results during indexing and compaction writes without setup acquiring a write lock.
- [x] New and reopened indexes retain incremental vacuum and compaction regression checks pass.
- [ ] Dogfooding search is verified after deploying the fix.

## Implementation plan

Configure persistent auto_vacuum and WAL mode once at index startup, in that order before schema creation, rather than for every pooled connection. Keep only connection-local pragmas in the DSN. Verify fresh numbered-search readers during a WAL write and vacuum mode across fresh connections/reopens, then run recall/web race suites and Forgejo CI/review.

## Notes

**agent:codex/t3code-8afe4a1e** at 2026-10-09T01:38:18Z

The standalone synthetic probe and new TestFreshSearchConnectionDuringIndexWrite reproduce SQLITE_BUSY with the original code (regression failed after 5.08 seconds). Moving persistent settings to startup fixes it. The vacuum persistence regression caught an initial ordering error: WAL can allocate the first page before auto_vacuum is configured; the corrected startup preserves the original auto_vacuum-before-WAL order. Both fresh-reader and new/reopened-vacuum regressions pass under the race detector (1.360 seconds). An earlier broader test invocation built against the superseded intermediate change was stopped; validation will run on the final code. No protected data or credentials were accessed.

**agent:codex/t3code-8afe4a1e** at 2026-10-09T01:40:59Z

Clean model review: https://git.local.sothr.com/terva-sh/lampi/pulls/210#issuecomment-28295, run 4dbcc2d0-c2a2-4afc-be39-ea3ba4c2af1c. Reviewed head 547066c2a111c0f1484be275105e8e0423cbafbd against base 10d86676f16f3879162102ac5a7c4d03a354a9af; no findings. Review checked the supplied diff and tests, with broader caller behavior and live deployment outside its evidence. Full recall/web race validation is running separately.

**agent:codex/t3code-8afe4a1e** at 2026-10-09T01:43:57Z

Final source validation passed: go test -trimpath=false -race ./internal/recall ./internal/web (271.048s and 43.961s), including FTS saturation recovery, rollback, online optimize, numbered paging and the fresh-connection regressions. go vet ./internal/recall and git diff --check passed. git ticket check has zero errors and only the existing unrelated long-title warning. AC4 remains pending until a protected dogfooding rollout and authenticated Search verification; this session cannot install with sudo because the terminal password is required.

## Summary

Persistent auto_vacuum and WAL setup moved to index startup in PR #210, preserving initialization order and allowing new pooled readers during index writes. Original SQLITE_BUSY failure reproduced; focused and full recall/web race checks pass, and model review is clean. Merge gates and dogfooding rollout/healthy Search verification remain outstanding. Successful d7dcde1 compaction sizes are recorded separately in TKT-01M4ET6TFCKN6H6CZRZNAPJA63.
