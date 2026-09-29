---
schema: 3
id: TKT-01M3N8FHQD44ER3CZGZQCTW6G2
title: "Dashboard: Allow returns to the device page, with a Back link"
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
assignees: []
milestone: null
parent: TKT-01M3N8F354GDK9D0CTQ2BMZBMY
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-29T00:19:21Z
updated_at: 2026-09-29T00:19:21Z
created_by:
  id: agent:claude-code/10adf304
  name: ""
updated_by:
  id: agent:claude-code/10adf304
  name: ""
extensions: {}
---

## Description

The first step, and small enough to ship on its own. Today **Allow…** on `/devices/{id}` opens `/profiles/NAME/edit` through a POST. The editor's only way out is Cancel to the profile page, and saving redirects to the profile page. An operator going through a device's refused projects loses their place on every one.

- Carry a `return` value from the device page through preview and save. Accept only a same-origin dashboard path, such as `/devices/{id}` or `/review`, and never an absolute URL, so it can't become an open redirect.
- The editor shows **Back to DEVICE** when a return is set, in place of Cancel to the profile.
- A successful save redirects to the return path, with a notice naming the rule added and the profile saved. Without a return it goes to the profile page, as today.
- A save refused as stale re-renders the editor and keeps the return.

## Acceptance criteria

- [ ] The editor opened from Allow shows Back to the device
- [ ] Saving from Allow redirects to the device page with a notice
- [ ] The return accepts only a dashboard path, and a test proves an absolute URL is refused
