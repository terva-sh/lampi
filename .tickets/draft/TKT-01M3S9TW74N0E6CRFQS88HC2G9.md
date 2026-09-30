---
schema: 3
id: TKT-01M3S9TW74N0E6CRFQS88HC2G9
title: "Search page: slimmer chrome, short hit times, stronger highlight"
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
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-30T13:59:59Z
updated_at: 2026-09-30T13:59:59Z
created_by:
  id: agent:claude-code/cb0b017c
  name: ""
updated_by:
  id: agent:claude-code/cb0b017c
  name: ""
extensions: {}
---

## Description

Web-only change, with no query-layer work. It moves the first result from about 550px down to about 250px, and makes each hit's time and match easy to read. The decisions are in the parent epic, under "Chrome" and "Hit rows".

- Hide the shared refresh bar on `/search`. Today the form sits inside `data-live`, so "Refresh now" redraws it and loses text that has been typed but not submitted (`internal/web/templates/page.html:11-12`).
- Use a smaller title and drop the description line. Keep the "Read-only view" badge.
- Shorten the coverage line to "N of M sessions searchable" and move it beside the results. The index time and the not-yet-indexed and normalizing detail go in a hover.
- Remove the help paragraph. The syntax hint belongs to the form ticket, and the date and UTC rule moves into the date field's hover.
- Show each hit's time short, as `Sep 28 00:40 UTC`, with the full ISO time on hover. The grouped view later drops the date when it matches the header.
- Give `mark` clearly more contrast in both themes (`internal/web/assets/lake.css:5`).

## Acceptance criteria

- [ ] The first result starts about 250px from the top at desktop width
- [ ] Refresh now no longer redraws the search form
- [ ] Hit times are short with the ISO time on hover
- [ ] Highlight passes a visible-contrast check in both themes
