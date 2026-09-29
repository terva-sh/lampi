---
schema: 3
id: TKT-01M3N8FHVNFEZ5N576RQ7CAG4E
title: "Dashboard: /review lists projects needing a decision across devices"
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
assignees: []
milestone: null
parent: TKT-01M3N8F354GDK9D0CTQ2BMZBMY
origin: null
dependencies:
  - TKT-01M3N8FHSG9BPCTH16J3NE5HXK
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-29T00:19:22Z
updated_at: 2026-09-29T01:14:42Z
created_by:
  id: agent:claude-code/10adf304
  name: ""
updated_by:
  id: agent:claude-code/10adf304
  name: ""
extensions: {}
---

## Description

One page listing every project that still needs a decision, across all devices, so onboarding doesn't mean visiting each device in turn.

- **`/review`**, linked in the header with a count of projects needing review. Viewers can see it. Only operators get the actions.
- **Needs review** is every refused project that is not hidden, not allowed by its device's profile, and allowable: not refused by a deny rule and not missing a cwd. It is grouped lake-wide by git remote or cwd. Each row shows the project, the devices holding it with their profiles, sessions, size, newest session, first seen, and the refusal reason. Sorted by first seen, newest first.
- **Allow pending**: projects whose device's profile now permits them but whose agent has not sent a newer inventory yet. Shown collapsed, so a just-allowed project doesn't look unhandled.
- **Denied**: projects a deny rule refuses. Listed for reference, with no Allow and a link to the profile.
- **Hidden** tab: hidden projects with who hid them, when and why, and **Unhide**.
- Strict devices appear as a line each with their refused session count and size. The line says the device doesn't name those projects.
- Filters: device, harness, profile. Everything works without JavaScript, like the rest of the dashboard.
- Each device's page links to `/review?device=ID`. Its own inventory table keeps working.
- `GET /api/web/v1/review` returns the same data as JSON, documented in `docs/web-api.md`.

## Acceptance criteria

- [ ] /review lists unhidden, unallowed, allowable refused projects grouped lake-wide, newest first
- [ ] Allow pending, Denied and Hidden are shown apart from Needs review
- [ ] The header links /review with its count
- [ ] The browser API returns the same queue
