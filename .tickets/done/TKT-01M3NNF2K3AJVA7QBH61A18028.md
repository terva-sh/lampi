---
schema: 3
id: TKT-01M3NNF2K3AJVA7QBH61A18028
title: "Bays: dashboard scoping, inbox view, move and release"
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
  - area/auth
assignees: []
milestone: null
parent: TKT-01M3N8KHW56GVZXHV0KBNSPEX1
origin: null
dependencies:
  - TKT-01M3NNF27AS0NCTG7N8XFDMWMK
  - TKT-01M3NNF2FEC5VKPV6Q8PKZGBQ9
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-29T04:06:17Z
updated_at: 2026-09-29T20:04:00Z
created_by:
  id: agent:claude-code/7859b064
  name: ""
updated_by:
  id: agent:claude-code/7859b064
  name: ""
extensions: {}
---

## Description

Dashboard support for bays. Design in the parent epic TKT-01M3N8KHW5 (Bays: segment one lake and route sessions to a bay).

### Scope

- Every view shows only the caller's read bays, using the scoping from the read-path child. Session views name the session's bays.
- An inbox view: counts, and the list of unsorted, held and refused sessions with their reasons.
- Admin actions: move one session between bays, and release a hold. These are the dashboard's first writes to session membership. They use the existing CSRF protection, require a sign-in in the last 10 minutes when they add a session to a bay, and queue audit events naming the admin, as raw reads and registration mints already do.
- Bulk move and rule editing stay on the CLI. The full triage flow is a separate follow-up.
- `docs/web-dashboard.md` records the new admin actions and bay scoping for viewers and operators, beside the existing admin section.

## Acceptance criteria

- [x] Views show only the caller's read bays
- [x] docs/web-dashboard.md documents the admin role and actions
- [x] An admin can move one session and release a hold, with CSRF protection, a recent sign-in when access is added, and an audit event naming the admin

## Implementation plan

One PR (bays/dashboard) stacked on the inbox child. The session page names the session's bays, filtered by the reader's scope (catalog.SessionBayNames takes a Scope). An admin page at /admin/bays lists bays with counts and the inbox with reasons (catalog.Inbox). It has two form actions: move one session (catalog.MoveSession) and release a hold (ReleaseHold now takes via). Both check the CSRF token, require webauth.Fresh because each adds a session to a bay, write via=web with the admin as actor, and flush the audit. Docs: the web-dashboard.md Bays section and the admin role line.

## Notes

**agent:claude-code/7859b064** at 2026-09-29T16:32:36Z

Gaps left on purpose:
- The JSON session API (/api/web/v1/sessions/{uid}) does not name bays yet; only the page does.
- Editing a device's bay grants from the dashboard, deferred here from TKT-01M3NNF24A, is not built. Grants stay on serve bays grant. Operators therefore cannot edit device grants at all, which satisfies that ticket's 'not outside its scope' AC trivially but leaves the feature for the triage follow-up TKT-01M3NNF2NN.
- The inbox has no counts per reason. The table length is the count.

**agent:claude-code/7859b064** at 2026-09-29T20:03:59Z

Review rounds, PR #163. Review 1469: an inbox session in no bay could not be moved from the dashboard. MoveSession now places one when moving from the default, as the bulk move does; the form offers 'no bay' as its source and is hidden only while a hold keeps the session for review. The release message names the bays the session is in afterwards, instead of claiming it got the bays it asked for. From #162 review 1468: the single move refuses a held session with ErrSessionHeld, shown as the 'held' problem on the page.

## Summary

Merged as #163 (45c8c53). The session page names the session's bays, only those the reader reads, and every other view is scoped by TKT-01M3NNF27A. /admin/bays (admin only) lists the bays and the inbox with the same reasons as serve bays inbox. An admin can move one session, including one in no bay from the default, and release a hold. Both need CSRF and a sign-in in the last 10 minutes, and both are audited via web with the admin as actor. A move refuses a held session. docs/web-dashboard.md has a Bays section. Tests: internal/web/bays_admin_test.go and internal/catalog/inbox_test.go.
