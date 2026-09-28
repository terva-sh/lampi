---
schema: 3
id: TKT-01M3JV462PY0VCMJHBB3GHEAEC
title: "Web: dark theme following the system, with a toggle"
type: task
status: in-progress
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
claim:
  actor: agent:claude-code/e4a47e8c
  branch: ops/dark-theme
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-e4a47e8c
  commit: c1515832c43cda466c3a488175c6a0c6453f5b7e
  session: null
  claimed_at: 2026-09-28T02:09:42Z
  expires_at: null
archive: null
created_at: 2026-09-28T01:47:29Z
updated_at: 2026-09-28T02:09:43Z
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

- [x] Every colour is a custom property; dark tokens apply by prefers-color-scheme
- [x] A system/light/dark toggle persists without inline script
- [x] Both themes meet WCAG AA contrast for text

## Notes

**agent:claude-code/e4a47e8c** at 2026-09-28T02:09:42Z

How the dark theme is built, and what each choice beat:

- **All colours are tokens.** Every colour literal in lake.css, about 40
  of them, now lives in custom properties declared on `:root`. A near
  duplicate became one token: several light borders are now
  `--line-soft`.
- **Dark applies two ways.** `:root[data-theme=dark]` is an explicit
  choice. `@media (prefers-color-scheme: dark)` with
  `:root:not([data-theme=light])` follows the system. CSS cannot share
  one block between a selector and a media query, so the dark block is
  written twice, and a test asserts the two copies match.
- **The remembered choice is applied before paint.** A small blocking
  `/assets/theme.js` sits ahead of the stylesheet, so a choice opposite
  to the system never flashes. An inline script lost: the CSP forbids
  it, and loosening the CSP for a theme is the wrong trade. Setting the
  theme from deferred lake.js lost too, because it paints first.
- **The toggle is one button cycling system, light, dark.** A select
  would need a label and more header room. It stays hidden without
  scripts.
- **Contrast is tested.** A test computes WCAG ratios from the tokens
  for every text/background pair in use, in both themes. It found the
  existing light theme's muted text at 4.44:1 on the page background,
  so `--muted` went from #667574 to #5f6e6d. Chip ink and warn ink were
  darkened a step for the same reason.
- **The auth error page** has no stylesheet. It gains
  `<meta name="color-scheme" content="light dark">` so the browser's
  defaults follow the system.

Checked in headless Chromium: overview and operations render in both
themes; the toggle cycles and persists across reloads; the console
shows no CSP errors.
