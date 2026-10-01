---
schema: 3
id: TKT-01M3S9TWGZBF1SCKTKTE976ABH
title: "Spike: time grouped search and the session walk on an index copy"
type: spike
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
  - TKT-01M3S9TWEKE9REXY3E7176JE2F
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-30T13:59:59Z
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

The sessions shape and the `session=` walk both bend a rule TKT-01M3F2PGH set on purpose: query plans must not sort every match. Measure before building either. The decisions are in the parent epic, under "Two result shapes".

Measure these on a copy of the live `search.db`, never against the live lake on the dev host:

1. **Grouped query.** For a common term, a medium term and a rare term, measure the time to produce 20 sessions ordered by their latest matching event, each with up to 3 hits in transcript order and a hit count capped at 50. Try both candidates: aggregate over the FTS matches, and walk sessions newest first with a per-session probe through `docs_session(session_uid,pos)`.
2. **Session walk.** Measure the time for `session=` plus text in transcript order, and check that SQLite tests the match per row instead of sorting.
3. **Short-term post-filter.** Measure the time for a common long term plus a short term (for example "the ui").

Record the timings, query plans, index size and SQLite version in a note. Recommend whether the sessions shape can stay well under 5 s, or whether the fallback applies: grouping each page in the template.

## Acceptance criteria

- [ ] Timings and plans for all three measurements are recorded in a note
- [ ] A go or fallback recommendation for the sessions shape is recorded
