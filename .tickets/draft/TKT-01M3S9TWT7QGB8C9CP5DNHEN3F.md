---
schema: 3
id: TKT-01M3S9TWT7QGB8C9CP5DNHEN3F
title: "Search: grouped sessions view as the web default"
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/search
assignees: []
milestone: null
parent: TKT-01M3S9TCEE0TKQFGS4DSV15ZAF
origin: null
dependencies:
  - TKT-01M3S9TWGZBF1SCKTKTE976ABH
  - TKT-01M3S9TWKZPRR114K462PD3YVD
  - TKT-01M3S9TWQ40PY6XYSHJZQNJCSC
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-30T14:00:00Z
updated_at: 2026-09-30T14:00:00Z
created_by:
  id: agent:claude-code/cb0b017c
  name: ""
updated_by:
  id: agent:claude-code/cb0b017c
  name: ""
extensions: {}
---

## Description

Make the sessions shape the web default. It depends on the spike's verdict, the session-identity fields and the `session=` filter. The decisions are in the parent epic, under "Two result shapes", "Order in the sessions shape" and "Paging".

- Add `view=sessions|events` to the query layer, so web and MCP share it. The web default is `sessions`. The page shows a "By session | All events" toggle.
- The sessions shape pages 20 sessions at a time, with a forward-only cursor over sessions.
- Order sessions newest first by their latest matching event, with no sort toggle.
- Each group shows the header from the session-identity ticket, a dim second line (harness, machines, hit count capped as "50+", UUID copy token), and up to 3 hits in transcript order.
- "Show all N" is a plain link to `view=events&session=…`.
- The whole page works with JavaScript off. Move the smoke test's 50-row JS-off check to `view=events`, and add a check for the grouped page.
- If the spike rules the shape out, build the fallback instead: group each flat page in the template. Record that in a note.

## Acceptance criteria

- [ ] The sessions view is the web default and the events view keeps the flat list
- [ ] Groups show header, second line and up to 3 hits in transcript order
- [ ] The grouped page and the events page both pass JS-off smoke checks
