---
schema: 3
id: TKT-01M4ESZRAJ5AG6ZCMV8162WDEV
title: Give Lampi a pond icon in browser tabs and shortcuts
type: task
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
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
  branch: feat/pond-icon
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-8afe4a1e
  commit: 92c2dbec1e9f04d0bdfc14f78213b8a1124749fc
  session: null
  claimed_at: 2026-10-08T22:27:29Z
  expires_at: null
archive: null
created_at: 2026-10-08T22:26:19Z
updated_at: 2026-10-08T22:27:29Z
created_by:
  id: agent:codex/t3code-8afe4a1e
  name: ""
updated_by:
  id: agent:codex/t3code-8afe4a1e
  name: ""
extensions: {}
---

## Description

Lampi currently appears as a generic icon in browser sidebars. Add a recognizable pond mark based on the dashboard waves and palette, with favicon discovery and saved-shortcut support.

## Acceptance criteria

- [ ] Browser pages declare a recognizable pond favicon.
- [ ] Unauthenticated favicon discovery works without an OIDC redirect.
- [ ] Vector and raster icons remain legible at tab and shortcut sizes.

## Implementation plan

Create a native SVG pond mark using the existing blue and cream palette, and rasterize it for ICO and Apple touch shortcuts. Serve root discovery files publicly and declare the formats in the shared page head. Keep the dashboard wave branding and avoid adding a frontend build or image dependency. Check small-size rendering and existing web coverage; complete Forgejo CI and review before merging.
