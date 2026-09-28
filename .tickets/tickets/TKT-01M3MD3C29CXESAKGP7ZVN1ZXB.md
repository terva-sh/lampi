---
schema: 3
id: TKT-01M3MD3C29CXESAKGP7ZVN1ZXB
title: Recall reclaim test can run past the CI timeout on a busy runner
type: bug
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area/search
  - area/ci
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim:
  actor: agent:claude-code/2cf53976
  branch: tests/recall-reclaim
  worktree: /home/sothr/workspace/git.local.sothr.com/terva-sh/lampi/.claude/worktrees/agent-ac8d4c4471fe969f0
  commit: ba571e14ea179aae042f50a8fb290cac4dcff1a2
  session: null
  claimed_at: 2026-09-28T18:13:04Z
  expires_at: null
archive: null
created_at: 2026-09-28T16:20:51Z
updated_at: 2026-09-28T18:13:04Z
created_by:
  id: agent:claude-code/aa1afd80
  name: ""
updated_by:
  id: agent:claude-code/2cf53976
  name: ""
extensions: {}
---

## Description

`TestReindexingKeepsTheIndexNearItsLiveSize` (`internal/recall/reclaim_test.go`), added under TKT-01M3KC2DD ("Search index grows by a third after re-normalizing every session"), hit the 10-minute `go test` timeout in Forgejo CI on PR #74, run 740, at commit bab740d. PR #74 doesn't touch `internal/recall`.

- At the timeout the test had been running for 9m52s. The goroutine dump showed it inside `_fts5IndexMerge`, called from `_fts5SpecialInsert`: an FTS5 `merge` command, still running.
- The same code passed CI on `main` (f50c57f) a minute earlier. Locally, the whole `internal/recall` package takes about 20s.
- Three CI jobs were running at once on the runner when it timed out.

So the test is intermittent. The likeliest cause is that merge work grows with how slow the runner is, or that a merge loop exits only on a condition a slow runner reaches late or never. Check whether the reclaim loop in `internal/recall` bounds its merge steps, and whether the test's corpus can be smaller and still show the index returning to near its live size.

## Acceptance criteria

- [ ] TestReindexingKeepsTheIndexNearItsLiveSize finishes well inside the CI timeout under concurrent jobs
- [ ] The reclaim loop's merge work is bounded, or the reason it need not be is recorded

## Notes

**agent:claude-code/2cf53976** at 2026-09-28T18:13:04Z

Root cause is not an unbounded merge. indexSession wrote one DELETE and one INSERT per changed row. Each docs write fires the fts trigger inside a savepoint, and FTS5 flushes a segment at every savepoint. Merging those per-row segments was most of the cost: 53% of -race CPU went to _fts5SavepointMethod -> FlushToDisk and automatic merges, and about 13% to the explicit reclaim merge. The reclaim loop is already bounded: mergePages 2000 per pass, about 8ms a call, and the first idle pass after a forced merge finds nothing. CI runs about 13x slower than the workstation, so 22s locally is about 300s on a busy runner.

Fix: docWriter in internal/recall/index.go batches deletes into DELETE ... WHERE id IN (...) and inserts into a multi-row INSERT. A batch holds up to 200 rows or 4 MiB. TestIndexScale went from 13.0s to 4.6s with the same 3 MiB peak heap. The reclaim test now runs 4 generations instead of 8, because with batching 8 generations no longer caught a missing forced merge (ordinary merge ended at 1.31x). With 4 generations: forced merge 1.2x passes, ordinary merge 2.9x fails, no merge 3.3x fails. The run is deterministic. Under -race the test went from 22-29s to 3-4s, and the whole package from about 35s to about 12s.

Alternatives: capping merge steps per pass would not help, since the merge is already bounded. Skipping under -race would hide a real production inefficiency. Keeping 8 generations would lose the regression check. Only shrinking the corpus leaves the per-row cost in production. Explicit batched writes to fts would change the schema (indexVersion bump and rebuild) for the same result.
