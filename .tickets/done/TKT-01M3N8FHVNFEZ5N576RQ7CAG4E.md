---
schema: 3
id: TKT-01M3N8FHVNFEZ5N576RQ7CAG4E
title: "Dashboard: /review lists projects needing a decision across devices"
type: task
status: done
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
updated_at: 2026-09-29T01:31:58Z
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

- [x] /review lists unhidden, unallowed, allowable refused projects grouped lake-wide, newest first
- [x] Allow pending, Denied and Hidden are shown apart from Needs review
- [x] The header links /review with its count
- [x] The browser API returns the same queue

## Implementation plan

internal/web/review.go builds reviewView from catalog.ReviewQueue. Each device copy's state comes from the device's effective profile via ResolveProfile, so per-device overrides slot in later: denied when the inventory reason is not allowable or the profile's deny rules now refuse it, allow_pending when the profile permits it, needs_review otherwise. A project row is split per state, so one repository can sit in two sections. Filters are device, harness and profile, strictly parsed; hidden is a tab. The header count comes from a Server carried in the request context (withServer in New), so every page's layout shows it without threading the server through renderStatus. Rejected: a TTL cache for the count. A home lake reads a few inventories, and invalidating the cache from every writer would be more code than the read it saves. returnPath now also accepts /review with a canonical filter, so Allow from the queue comes back to it.

## Summary

/review and GET /api/web/v1/review list refused projects across active devices, grouped by git remote or cwd, in Needs review, Allow pending, Denied, a Hidden tab and strict-device totals, with device, harness and profile filters. The header's Review link shows the Needs review count on every page. Operators get Allow in PROFILE… per device copy, which returns to the queue with its filters and a saved notice. Each device page links to its slice of the queue. Docs: web-dashboard.md Review and web-api.md Review queue. Tests: TestReviewListsProjectsNeedingADecision, TestReviewAPI, TestReviewAllowComesBackToTheQueue, TestReturnPathAcceptsTheReviewQueue. Checked visually with a headless render of the test fixture.
