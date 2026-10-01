---
schema: 3
id: TKT-01M3V4BFDNKA4QCED621N4W6A1
title: Adopt the shared terva-sh/design foundation in the dashboard
type: task
status: draft
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
claim: null
archive: null
created_at: 2026-10-01T07:02:40Z
updated_at: 2026-10-01T07:02:41Z
created_by:
  id: agent:claude-code/310d0296
  name: ""
updated_by:
  id: agent:claude-code/310d0296
  name: ""
extensions: {}
---

## Description

## Acceptance criteria

- [ ] go.mod requires github.com/terva-sh/design at a tagged version, and the dashboard serves its stylesheet from the embedded module ahead of lake.css.
- [ ] Light and dark use data-scheme on <html>. lake.js and theme.js agree, theme.js still loads before the stylesheet, and a stored lampi-theme choice still applies.
- [ ] lake.css declares only lampi's own tokens, and no rule outside it holds a literal colour.
- [ ] TestThemesMeetContrast covers the app-owned tokens on the new grounds, in both schemes.
- [ ] e2e/web-smoke.mjs passes.

## Summary

terva-sh/design (git.local.sothr.com/terva-sh/design, module github.com/terva-sh/design) now holds one design foundation for the terva-sh web apps: terva, lampi, ketju and git-ticket-canvas. It sets a light scheme (Birch) and a dark scheme (Tar), the status colours, and the type, radius and space scales. Each app keeps its own accent, and lampi's stays its blue: #27509B light and #9DB8F0 dark.

Today the dashboard's palette is hand-written in internal/web/assets/lake.css: warm paper, with light as the default. lampi has no frontend build, so it can serve design.CSS("lampi") straight from the embedded module, ahead of lake.css. Then lake.css maps or replaces --paper, --surface, --surface-2, --ink, --muted, --line, --line-soft, --accent, --accent-strong, --on-accent, --focus, --ok, --warn, --bad and their backgrounds with the --ui-* roles.

The light and dark switch moves from data-theme to data-scheme on <html>. That is the org convention, and data-theme stays free for palette presets. lake.js, theme.js and TestPagesLoadThemeBeforePaint change together. A choice stored under localStorage lampi-theme before the change must still apply.

App-owned tokens stay in lake.css and are rechecked against the new grounds: the chart colours (--bar, --bar-neg, --axis, --hatch, --dot, --meter), --assistant, --tool, --highlight, --mark and the chips. The serif headings stay lampi's own.

People will see this change, though it is a small one, because lampi's paper is already close to Birch.

Before starting: github.com/terva-sh/design needs its public mirror and a tagged release, so that the module resolves.
