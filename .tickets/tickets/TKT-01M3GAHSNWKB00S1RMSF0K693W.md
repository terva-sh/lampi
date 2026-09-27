---
schema: 3
id: TKT-01M3GAHSNWKB00S1RMSF0K693W
title: "Registration codes: audit events after a commit can be lost"
type: bug
status: ready
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
created_at: 2026-09-27T02:19:20Z
updated_at: 2026-09-27T22:38:20Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/e4a47e8c
  name: ""
extensions: {}
---

## Description

Finding-1 from terva-review 992 on PR #17 (TKT-01M3FHHBJ, Registration codes), deferred at the owner's direction on 2026-09-27 so the onboarding stack could merge.

`RecordExpiries` in internal/catalog/registrations.go sets `expiry_recorded_at` and commits before either caller appends the `registration.expired` line. If that append then fails, for example because the audit file cannot be opened, the code is never returned again, so its expiry line is lost for good. `/v1/register` only logs the failure; `serve register` reports it and says the line will not be retried. That was a known cost when the fix landed, but it breaks the "every expiry reaches the audit log once" guarantee.

Fix: keep the event eligible until the append succeeds. For example, select the rows, append the lines, then mark them, and accept a possible duplicate line after a crash in between: a duplicate is better than a lost event. Alternatively, a two-phase marker (claimed, then recorded) can be reclaimed after a timeout. Test with an audit file that cannot be written: the expiry must be recorded once it can be.

## Notes

**agent:claude-code/e4a47e8c** at 2026-09-27T02:31:13Z

Widened by terva-review 996 on PR #17 (finding-2), deferred with this ticket at the owner's direction. The redemption itself has the same gap. `/v1/register` commits the device and the spent code, and only then appends `registration.redeemed` and the device-binding events. If that append fails, the handler still returns success, and nothing can recreate those events later, since the code cannot be replayed.

So the general problem is that every audit event written after its catalog commit can be lost when the append fails: expiry, redemption, and binding. The fix should cover all of them. One option is an outbox of pending audit events kept in the catalog and written in the same transaction, then appended and cleared, and retried on the next write or at serve start. That gives at-least-once delivery, where a duplicate line is acceptable and a lost one is not.

**agent:claude-code/e4a47e8c** at 2026-09-27T22:38:20Z

Deferred behind open PR #40 (catalog/head-updates, TKT-01M3F2RKG), which adds catalog schema 8. The outbox this ticket needs is a catalog migration too, and two branches each appending schema 8 would collide. Picking it up once #40 lands, on top of it. Plan: an audit_outbox table written in the same transaction as the catalog change (expiry marks, redemption, device binding), appended and cleared after commit, retried on the next write and at serve start: at-least-once, with a duplicate line accepted and a lost one not.
