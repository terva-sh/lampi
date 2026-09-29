---
schema: 3
id: TKT-01M3PTMWF6TGWRVKB5NBA2TBGM
title: "Conflicts page: explain a row and what to check"
type: task
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
assignees: []
milestone: null
parent: TKT-01M3PTMA4C1XD80THS0AXEKK1Y
origin: null
dependencies:
  - TKT-01M3PTMKG9Y2S51V5Q59YZ0P7D
blocks_on: none
references: []
claim:
  actor: agent:claude-code/cd41c9ac
  branch: feat/conflicts-page
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-fdd1a9d1
  commit: 9260d9516c131635e785b2d3ed17b1ae53588083
  session: null
  claimed_at: 2026-09-29T15:03:57Z
  expires_at: null
archive: null
created_at: 2026-09-29T14:56:05Z
updated_at: 2026-09-29T15:11:36Z
created_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
extensions: {}
---

## Description

The Conflicts page says "Divergent copies are preserved. The original
head stays in place." and lists rows. Nothing tells a reader what a row
means, whether it needs anything, or what to look at.

### Approach

- A short explanation above the list: what a divergent copy is, that
  nothing was lost or merged, and the signs worth checking (the copy
  and the head came from different machines; the copy is shorter than
  the head; many copies at one path, which means the session stopped
  moving).
- Each row shows the machines that posted the copy and the head, as
  `terva-lampi conflicts` already does, and the head's size beside the
  copy's.
- An empty page says there is nothing to do.
- A control to show resolved conflicts, with each row's resolution.

## Acceptance criteria

- [x] The page explains what a divergent copy is and what to check
- [x] Each row names the machines that posted the copy and the head, and both sizes
- [x] An empty page says there is nothing to do
- [x] Resolved conflicts can be shown, each with its resolution

## Notes

**agent:claude-code/cd41c9ac** at 2026-09-29T15:11:36Z

Verified in headless Chromium against the smoketest (open and resolved views) and in the browser e2e, whose conflicts step now checks 5 open rows, the shorter badge, the copies-at-path count, device naming, and 6 rows with resolved. The e2e then fails at 'mobile transcript overflows', which fails identically on origin/main: filed TKT-01M3PVGY. Smoketest: dropped the i%17 seeding, which made copies whose digest equalled their own head (impossible on a real lake), and seeded four real shapes on existing Claude sessions so the session count stays 123.
