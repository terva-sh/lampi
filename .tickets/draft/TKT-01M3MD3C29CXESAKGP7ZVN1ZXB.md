---
schema: 3
id: TKT-01M3MD3C29CXESAKGP7ZVN1ZXB
title: Recall reclaim test can run past the CI timeout on a busy runner
type: bug
status: draft
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
claim: null
archive: null
created_at: 2026-09-28T16:20:51Z
updated_at: 2026-09-28T17:59:55Z
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

**agent:claude-code/2cf53976** at 2026-09-28T17:59:55Z

More evidence (2026-09-28), filed in TKT-01M3MC40FF before this ticket was found: timed out again on Forgejo runs 696, 711 and 718 (PRs #66, #68, #71), at 9m39s-9m50s, with six pipelines running at once, and again on run 711 attempt 2. Locally, 'go test -race -count=1 -run TestReindexingKeepsTheIndexNearItsLiveSize ./internal/recall/' took 308s on an idle workstation. That is far above the package's 20s noted above, so -race makes it much slower.
