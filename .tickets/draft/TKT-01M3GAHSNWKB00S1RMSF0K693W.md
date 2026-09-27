---
schema: 3
id: TKT-01M3GAHSNWKB00S1RMSF0K693W
title: "Registration codes: keep an expiry eligible until its audit line is written"
type: bug
status: draft
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
updated_at: 2026-09-27T02:19:20Z
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
