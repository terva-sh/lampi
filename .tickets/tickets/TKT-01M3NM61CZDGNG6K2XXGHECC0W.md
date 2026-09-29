---
schema: 3
id: TKT-01M3NM61CZDGNG6K2XXGHECC0W
title: Admin role and raw artifact access
type: epic
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area/auth
  - area/server
  - policy
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim:
  actor: agent:claude-code/cd41c9ac
  branch: web/raw-view
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-fdd1a9d1
  commit: 94243ccc80f3e55c6f546a8aa67c570236a258d4
  session: null
  claimed_at: 2026-09-29T03:52:22Z
  expires_at: null
archive: null
created_at: 2026-09-29T03:43:53Z
updated_at: 2026-09-29T03:52:22Z
created_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
extensions: {}
---

## Description

Let a lake admin read raw session artifacts: in the dashboard, and
through tokens for tools that have no browser session. Raw bytes are the
least filtered copy in the lake (ruleset v2 quarantines a hit but never
rewrites the file), so everything here is gated on a new `admin` role
above `operator`.

The owner decided on 2026-09-28 to reverse the "raw blobs out of scope"
line in `docs/web-ui-plan.md`, now that the dashboard has roles. The
trigger was a terva session whose normalization failed. The dashboard
showed no transcript and no way to see the input the projector choked
on.

### Children, in order

1. TKT-01M3NKZT6N (Dashboard: admin role above operator)
2. TKT-01M3NKY2V3 (Dashboard: admin-only raw artifact view), which
   depends on 1
3. The raw-read tokens ticket, which depends on 2 because it serves the
   same artifact reads

Each child lands as its own PR, so that terva-review can read every
change.

### Related work elsewhere

- TKT-01M3N8KHW5 (Bays) designs the admin/operator split this epic
  builds. As of 2026-09-28 it is on the unpushed branch
  `t3code/share-ponds-within-sessions`.
- TKT-01M3KAMD1Z (Lake-backed sessions: read path, token scope,
  byte-exact reads) plans read tokens more broadly, on the unpushed
  branch `t3code/session-lake-epic`. Raw-read tokens are the narrow
  first case, built so that they can become one permission in its
  model.

## Acceptance criteria

- [ ] All child tickets are done
- [ ] docs/web-ui-plan.md, docs/web-dashboard.md and docs/web-api.md describe the admin role, raw view and raw-read tokens
