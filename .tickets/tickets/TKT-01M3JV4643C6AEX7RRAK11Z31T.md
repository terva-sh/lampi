---
schema: 3
id: TKT-01M3JV4643C6AEX7RRAK11Z31T
title: "Web design pass: propose four directions for the owner"
type: spike
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area/ops
  - question
assignees: []
milestone: null
parent: TKT-01M3JV3TRWB6JQQGY1F84JCFDE
origin: null
dependencies:
  - TKT-01M3JV462PY0VCMJHBB3GHEAEC
blocks_on: none
references: []
claim:
  actor: agent:claude-code/e4a47e8c
  branch: ops/dark-theme
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-e4a47e8c
  commit: 4a0eeef70a69f094c8d1307fe3a6d4815817a36f
  session: null
  claimed_at: 2026-09-28T02:12:41Z
  expires_at: null
archive: null
created_at: 2026-09-28T01:47:29Z
updated_at: 2026-09-28T02:12:42Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/e4a47e8c
  name: ""
extensions: {}
---

## Description

The site deserves a second design pass. Before building one, propose four
directions for the owner to choose between. One may be a close variation
of the current design.

Each proposal:

- restyles the same real pages (overview, sessions, operations,
  transcript) in both themes
- comes with screenshots and a short rationale covering its
  typography, density, colour and navigation

The owner picks a direction, or a mix of them. Implementation follows in
its own ticket.

## Acceptance criteria

- [x] Four directions rendered on real pages in both themes, with screenshots
- [ ] The owner has chosen a direction

## Notes

**agent:claude-code/e4a47e8c** at 2026-09-28T02:12:42Z

Four directions, each written as a stylesheet laid over the token layer
from TKT-01M3JV462. Each was rendered on the real overview, operations
and sessions pages (smoketest data, 1280px, light and dark) and put on a
contact sheet.

The CSS, the screenshots, and the scripts that made them sit outside the
repository in `~/.local/state/agent-handoffs/lampi/design-proposals/`.
Every direction is CSS only: the markup stays as it is, so whichever
wins lands as a change to lake.css plus small template tweaks.

- **A · Refined Lake.** A close variation of the current site:
  - Keeps the teal, Inter and the top navigation.
  - Soft elevation in place of plain outlines; a sticky, translucent
    header.
  - Pill-shaped highlight on the current nav item; uppercase stat labels.
  - Tabular figures; row hover; rounder badges.
  - Lowest risk and least change.
- **B · Console.** An operator's instrument panel:
  - A left sidebar replaces the top bar.
  - Monospace figures and labels; dense tables; squared corners.
  - Signal-green accent; dark-first.
  - The best fit for operations and debugging; the least inviting for
    reading transcripts.
- **C · Field Notebook.** An archive you read:
  - Serif headlines and ledger-style serif numerals on warm paper.
  - Ink-blue accent; hairline rules instead of boxed cards.
  - Wider measure and more air.
  - The best for browsing and reading sessions; the lowest data density.
- **D · Graphite.** A quiet monochrome tool:
  - Neutral greys, black and white, with violet reserved for data and
    focus.
  - Centred pill navigation; big numerals; generous radius.
  - The most contemporary look. It drops the teal identity; the ≈ mark
    stays, in violet.

Found while rendering: the new theme button makes the header's account
area wrap at 1280px. The dark-theme commit now fixes it with nowrap. A,
C and D would still need the account area rebalanced; B moves it into
the sidebar.

Waiting on the owner's choice. AC2 stays open until then.
