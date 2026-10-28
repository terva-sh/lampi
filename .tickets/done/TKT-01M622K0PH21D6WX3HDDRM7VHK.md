---
schema: 3
id: TKT-01M622K0PH21D6WX3HDDRM7VHK
title: "CI: race tests time out on a shared runner"
type: bug
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area/ci
  - area/search
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-28T20:17:55Z
updated_at: 2026-10-28T21:24:00Z
created_by:
  id: agent:claude-code/d4b24133
  name: ""
updated_by:
  id: agent:claude-code/d4b24133
  name: ""
extensions: {}
---

## Description

Forgejo's Lint and Test job runs `go test -race ./...` with Go's default 10-minute per-package timeout. `internal/recall` takes about 270s under `-race` on a quiet 12-core workstation (2026-10-28), and `TestOptimizeRecoversSaturatedSegmentsAndRetriesFailedSessions` alone takes about 195s of that. On the shared runner, with several pull requests' CI running at once, the package passes 600s and the job fails with `panic: test timed out after 10m0s`.

On 2026-10-28 this failed #216 four times in a row and #223 once, while main's own runs passed when they landed in a quiet window. #216 does not slow the package: 269s against main's 274s under `-race`. The failure says nothing about the change under test, so it blocks merges under the rule that Lint and Test must be green.

### What changes

- The race step passes `-timeout 25m`, so a loaded runner fails only on a hung test.
- The slow Optimize test is a separate question: whether it can do the same work with fewer sessions or segments. This ticket records it and does not change it.

## Acceptance criteria

- [x] The race step in .forgejo/workflows/ci.yml passes -timeout 25m
- [x] The slow Optimize test is recorded with its timing

## Implementation plan

Pass -timeout 25m to the race step in .forgejo/workflows/ci.yml, with a comment giving the timing. Leave the slow test as it is and record its timing here. Considered and rejected: marking the Optimize test testing.Short and passing -short in CI, which would drop real coverage of segment recovery from the only gate that runs it under -race; a job-level timeout-minutes, which does not change Go's per-package 10m.

## Notes

**agent:claude-code/d4b24133** at 2026-10-28T20:18:07Z

Timing under go test -race -count=1 ./internal/recall on the development workstation (12 cores, load about 1.5), 2026-10-28. On main 39a2bea's parent 789fec3 the package took 274s, and TestOptimizeRecoversSaturatedSegmentsAndRetriesFailedSessions 197s, TestForcedMergeContinuesWhileNewSegmentsArrive 28s, TestOptimizeFreesTheEntriesOfRemovedRows 11s. On #216's head the package took 269s and the Optimize test 193s. With the Optimize test at two thirds of the package, it is the place to look if the package has to get faster.

**agent:claude-code/d4b24133** at 2026-10-28T20:18:18Z

Correction to the timing note: the main measurement ran on 39a2bea itself, not on its parent.

## Summary

Landed in #225. The race step passes -timeout 25m. After it, #216 and #217 passed Lint and Test on the first try where #216 had timed out four times. The slow Optimize test's timing is in the notes for whoever makes internal/recall faster.
