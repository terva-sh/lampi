---
schema: 3
id: TKT-01M3NNF2K3AJVA7QBH61A18028
title: "Bays: dashboard scoping, inbox view, move and release"
type: task
status: draft
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
updated_at: 2026-09-29T14:51:58Z
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

- [ ] Views show only the caller's read bays
- [ ] docs/web-dashboard.md documents the admin role and actions
- [ ] An admin can move one session and release a hold, with CSRF protection, a recent sign-in when access is added, and an audit event naming the admin
