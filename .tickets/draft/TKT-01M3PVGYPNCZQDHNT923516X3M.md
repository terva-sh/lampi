---
schema: 3
id: TKT-01M3PVGYPNCZQDHNT923516X3M
title: "Browser smoke fails: mobile transcript overflows"
type: bug
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
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
created_at: 2026-09-29T15:11:25Z
updated_at: 2026-09-29T15:11:25Z
created_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
extensions: {}
---

## Description

`node e2e/web-smoke.mjs` fails on main (origin/main 80e66de era, run
2026-09-29 with playwright 1.58.2) at the assertion "mobile transcript
overflows" (e2e/web-smoke.mjs, the 390px transcript check): the
transcript page is wider than the viewport on mobile.

Found while running the smoke for TKT-01M3PTMWF6; the same failure on a
clean worktree of origin/main, so it predates that change. The browser
smoke is not part of `just ci`, which is why nothing caught it.

Likely suspects: the global nav has grown (Review, Profiles,
Registrations, Read tokens, the theme button) and the header already
scrolls horizontally at 1280px wide; or a transcript element added since
the check was written.

### Also worth deciding

Whether the browser smoke should run somewhere automatically, since a
failure here went unnoticed.
