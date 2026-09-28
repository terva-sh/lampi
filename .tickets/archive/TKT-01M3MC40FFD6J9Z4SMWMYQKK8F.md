---
schema: 3
id: TKT-01M3MC40FFD6J9Z4SMWMYQKK8F
title: Recall reindex test nears the 10m test timeout under -race
type: bug
status: archived
status_reason: null
priority: high
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
archive:
  archived_at: 2026-09-28T18:00:01Z
  from_status: draft
  reason: duplicate
created_at: 2026-09-28T16:03:43Z
updated_at: 2026-09-28T18:00:01Z
created_by:
  id: agent:claude-code/2cf53976
  name: ""
updated_by:
  id: agent:claude-code/2cf53976
  name: ""
extensions: {}
---

## Description

`TestReindexingKeepsTheIndexNearItsLiveSize` in `internal/recall` runs close to `go test`'s 10-minute default timeout under `-race`, and on a busy runner it exceeds it.

### Evidence (2026-09-28)

- Forgejo CI runs 696, 711 and 718 failed with `panic: test timed out after 10m0s` while this test was running: at 9m50s, 9m39s and 9m47s. Six PR pipelines were running at once. Those runs were for lake-managed config PRs #66, #68 and #71, none of which touch `internal/recall`.
- Locally, `go test -race -count=1 -run TestReindexingKeepsTheIndexNearItsLiveSize ./internal/recall/` took 308s on an idle workstation.
- CI on `main` passed (6m7s) when it ran alone.

The test was added with TKT-01M3KC2DD (Search index grows by a third after re-normalizing every session).

### Options

- Shrink the corpus the test reindexes, keeping the growth ratio it asserts.
- Skip it under `-short` or `-race`, and run it in a separate job without `-race`.
- Give the recall package an explicit `-timeout` in CI. This only hides the cost.

## Acceptance criteria

- [ ] The test runs well under the timeout on the CI runner with -race

## Notes

**agent:claude-code/2cf53976** at 2026-09-28T17:59:55Z

Duplicate of TKT-01M3MD3C, which has the earlier analysis. The evidence from this ticket is copied there. Archived.

**agent:claude-code/2cf53976** at 2026-09-28T18:00:01Z

archived from draft: duplicate
