---
schema: 3
id: TKT-01M3K0Q900PVKSJT5825BHEAD2
title: "Web: implement the Field Notebook design"
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/ops
assignees: []
milestone: null
parent: TKT-01M3JV3TRWB6JQQGY1F84JCFDE
origin: null
dependencies:
  - TKT-01M3JV4643C6AEX7RRAK11Z31T
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-28T03:25:17Z
updated_at: 2026-09-28T03:33:10Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/e4a47e8c
  name: ""
extensions: {}
---

## Description

Implement the Field Notebook direction the owner chose in TKT-01M3JV464:

- serif headlines, and serif numerals set like a ledger
- warm paper in light, warm charcoal in dark
- an ink-blue accent
- hairline rules in place of boxed cards
- a wider measure

The proposal stylesheet only laid overrides on top of the current
design. This ticket changes the tokens and rules in lake.css itself, so
that every page holds up: overview, sessions, detail, transcript,
search, activity, operations, conflicts and registrations. Both themes
keep WCAG AA contrast, and nothing overflows from 320 to 1440px.

## Acceptance criteria

- [x] lake.css carries the Field Notebook tokens and rules in both themes
- [x] Every page renders without overflow from 320 to 1440px in both themes
- [x] The contrast test passes for both themes

## Notes

**agent:claude-code/e4a47e8c** at 2026-09-28T03:29:31Z

How the Field Notebook design was built:

- **Changed lake.css in place instead of layering the proposal's
  overrides on top.** The theme blocks now hold the Field Notebook
  palettes. Panels, stats, filters and tables use rules instead of
  boxes: a top rule in ink, headings and figures in a serif, and
  italic table headers. The page measure narrows to 1180px.
- **Badges keep a tinted background with an ink border.** The proposal
  made them transparent. Keeping the tint preserves quick scanning of
  state, and lets the existing contrast pairs still cover them.
- **Fonts come from system serif stacks: Iowan Old Style, Charter,
  Source Serif, then Georgia.** Self-hosting a web font was rejected:
  it adds a download on every first visit, and the CSP would need a
  font-src.
- **The light theme's colours were tuned for AA on the warm paper.**
  Muted is #5b574f, and chip, warning and bad ink were darkened a
  step. The contrast test passes in both themes.

Checked in headless Chromium. Every page type rendered in both themes:
overview, sessions, detail, transcript, search, activity, operations,
conflicts and registrations. No page overflows at 320, 390, 768, 1024,
1280 or 1440px. One fix came out of the renders: the SESSION LAKE label
had inherited the brand's serif italic.

docs/images now holds screenshots of the new design at the same size
as before, 2560×1600 at 2× scale.

## Summary

Landed in PR #53. The dashboard now uses the Field Notebook design in both themes. It uses serif headings and figures, warm paper, an ink-blue accent, and rules instead of boxes, on a 1180px measure. Contrast is AA, and nothing overflows from 320 to 1440px. The docs screenshots were refreshed.
