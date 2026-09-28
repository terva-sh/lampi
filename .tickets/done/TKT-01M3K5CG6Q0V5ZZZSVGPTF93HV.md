---
schema: 3
id: TKT-01M3K5CG6Q0V5ZZZSVGPTF93HV
title: Run Terva reviews from the v0.5.0 reviewer image
type: chore
status: done
status_reason: null
priority: normal
due_on: null
labels:
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
created_at: 2026-09-28T04:46:47Z
updated_at: 2026-09-28T04:49:31Z
created_by:
  id: agent:claude/t3code-45332409
  name: ""
updated_by:
  id: agent:claude/t3code-45332409
  name: ""
extensions: {}
---

## Description

Move this repository's Terva review from the terva-action-code-review v0.3.0 image to v0.5.0 (https://git.local.sothr.com/terva-sh/terva-action-code-review/releases/tag/v0.5.0), pinned by digest `sha256:64a7ba59ca8a0932e2b4d4cf8e50a2231f46fa6eaefa046acbef84f2a2886252`.

### What it gains

- **Up to 1 MiB of context,** not a fixed 256 KiB, so a large PR is reviewed rather than stopped at `context_limit`.
- **A change index** in every prompt.
- **Fallback models** from `TERVA_REVIEW_FALLBACKS`, now read by the workflow.
- **Decided findings** reach the reviewer.
- **A carry of every gate** in one dispatch.

Every profile digest and request key moves, so the first review of each open PR after this merges runs fresh. The allowlist, the concurrency group and the other settings stay as they are.

Part of terva-action-code-review TKT-01M3K5B2YX ('Move the consumers to the v0.5.0 image').

## Acceptance criteria

- [x] The review workflow runs the v0.5.0 image by digest and reads TERVA_REVIEW_FALLBACKS
- [x] A review of this change from its branch is recorded

## Notes

**agent:claude/t3code-45332409** at 2026-09-28T04:49:31Z

PR #55 (https://git.local.sothr.com/terva-sh/lampi/pulls/55), head f993d3a. Terva review from the PR's own branch, so the v0.5.0 image reviewed its own installation: request review-v0.5.0, run 07627348-75a1-49e1-bd67-7f8ba4f8ad22, terva-review/code success with no findings. Merge authorized by the maintainer on 2026-09-28.

## Summary

The Terva review runs terva-action-code-review v0.5.0 (sha256:64a7ba59, commit f3a857b) and reads TERVA_REVIEW_FALLBACKS, shipped in #55. Its branch review was clean.
