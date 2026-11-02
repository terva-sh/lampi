---
schema: 3
id: TKT-01M6ETZV7C0P5F2RDKQPAQCCFW
title: "Profile editor: move rules, sort by folder and owner, insert by group"
type: task
status: done
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
updated_at: 2026-11-02T20:15:29Z
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

- [x] Rules move up and down without scripts, and drag where scripts run; the order saves
- [x] Sort by folder and owner applies to the form and previews
- [x] Allow inserts a new rule beside its folder or owner instead of at the end
- [x] Tests and docs cover ordering

## Notes

**agent:claude-code/t3-43103b81** at 2026-11-02T19:47:46Z

Built on t3/profile-editor-order, stacked on #242. Move posts move=list.N.up|down and Sort posts sort=list. reorder() applies them in readRules, only when every row read, since raw rows are kept by index. Drag uses HTML5 drag events on a handle and renumbers the rows' field names and button values, then hides the group headers, which the next preview draws again. withRules now places each added rule after the last kept rule with the same groupKey, else where the first rule it replaced stood, else at the end. Rules aimed at one place keep add's order. The handle stays in the grid with visibility:hidden until the script runs, so the columns line up without scripts. GOFLAGS=-mod=mod just ci passes.

**agent:claude-code/t3-43103b81** at 2026-11-02T19:55:10Z

terva-review on #243 (run 2271): after a drag, the Narrow buttons kept the index of the rule the page was drawn with. The server re-diagnoses the posted order, so that index could name a different rule, and the same holds for the Health panel's fixes. Renumbering them in the page would mean mapping normalized doctor indices, so a drag now disables every narrow, fold, folder and doctor button until the next preview, and the notice says so. Remove, move and pick values are renumbered with the rows. I checked this in headless Chromium by simulating a drag of rule 0 onto rule 3: names and Remove renumbered to allow.2, all Narrow and doctor buttons disabled, notice shown.

## Summary

Merged in #243. Rules move with up and down buttons, and by dragging a handle where scripts run; Sort by folder and owner orders folders by path, then remotes. Order is the operator's, since it changes nothing an agent does. withRules puts a rule Allow adds after the last rule of its folder or owner, or where the first rule it replaces stood. A drag disables the doctor's fixes until the next preview, since they name rules by position.
