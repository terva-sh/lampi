---
schema: 3
id: TKT-01M6ETZV7C0P5F2RDKQPAQCCFW
title: "Profile editor: move rules, sort by folder and owner, insert by group"
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
  - policy
assignees: []
milestone: null
parent: null
origin: null
dependencies:
  - TKT-01M6ETZV536S1Z3SP2D5KZ4GJX
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-11-02T19:15:14Z
updated_at: 2026-11-02T19:15:15Z
created_by:
  id: agent:claude-code/t3-43103b81
  name: ""
updated_by:
  id: agent:claude-code/t3-43103b81
  name: ""
extensions: {}
---

## Description

The owner, 2026-11-02, on the profile editor: "It would be nice if I could move the rules around (not sure if it impacts order but visually I would like to group items together by default by path or project and then overriden by the user ordering."

Rule order has no effect on matching: any deny rule refuses, then any allow rule admits (config.Projects.Permitted). So the stored order can be the operator's order. The owner chose "Grouped, then your order".

### What changes

- Each rule can move up or down. Buttons work without scripts, and dragging works where scripts run. The order saves with the profile.
- **Sort by folder and owner** puts the rules in path order, grouping each folder's and owner's rules together. It applies to the form and previews, like the other fixes.
- A rule added by Allow (review queue, device page, editor offers) is inserted after the last rule of its folder or owner, or in path order when it has none, rather than at the end. A grouped list stays grouped without overriding an order the operator set.

Rejected: a stored flag saying whether the operator ordered the list by hand. It would change the profile document that agents read, and agents refuse unknown fields.

## Acceptance criteria

- [ ] Rules move up and down without scripts, and drag where scripts run; the order saves
- [ ] Sort by folder and owner applies to the form and previews
- [ ] Allow inserts a new rule beside its folder or owner instead of at the end
- [ ] Tests and docs cover ordering
