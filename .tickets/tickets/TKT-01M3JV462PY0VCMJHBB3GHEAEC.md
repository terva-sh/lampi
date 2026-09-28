---
schema: 3
id: TKT-01M3JV462PY0VCMJHBB3GHEAEC
title: "Web: dark theme following the system, with a toggle"
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/ops
assignees: []
milestone: null
parent: TKT-01M3JV3TRWB6JQQGY1F84JCFDE
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-28T01:47:29Z
updated_at: 2026-09-28T01:47:35Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/e4a47e8c
  name: ""
extensions: {}
---

## Description

The dashboard has only a light theme. Add a dark one:

- It follows `prefers-color-scheme` by default, with a toggle (system,
  light, dark) remembered in localStorage.
- Every colour moves to a CSS custom property first, including those in
  charts, badges, meters and focus rings, so the theme is one block of
  tokens. That token layer is also what the design pass builds on.
- Contrast holds WCAG AA in both themes.
- The strict CSP stays as it is: no inline script or style.

## Acceptance criteria

- [ ] Every colour is a custom property; dark tokens apply by prefers-color-scheme
- [ ] A system/light/dark toggle persists without inline script
- [ ] Both themes meet WCAG AA contrast for text
