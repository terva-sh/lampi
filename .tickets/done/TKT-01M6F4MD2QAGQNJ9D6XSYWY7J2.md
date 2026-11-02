---
schema: 3
id: TKT-01M6F4MD2QAGQNJ9D6XSYWY7J2
title: "Health panel: group findings by kind with a bulk fix, offer temp deny"
type: task
status: done
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
created_at: 2026-11-02T22:03:45Z
updated_at: 2026-11-02T22:54:45Z
created_by:
  id: agent:claude-code/t3-43103b81
  name: ""
updated_by:
  id: agent:claude-code/t3-43103b81
  name: ""
extensions: {}
---

## Description

The live default profile's Health panel lists 15 temporary-folder rules one after another, each with its own "Remove and hide 1 project and preview" button (owner's screenshot, 2026-11-02, after the v0.11.1 deploy). Clearing them takes 15 previews. The owner asked for a way to match these and act on them in bulk.

### Owner's choices (2026-11-02)

- **Grouped Health findings**, not a pattern box in the editor. Findings of one kind collapse into one row with the shared message, a count, one bulk button, and the per-rule findings in an expandable list that keeps their own buttons.
- **Offer a deny pattern, not by default.** After temporary rules are removed, or while temporary findings show, the editor offers a deny rule for their temporary roots (`/tmp`, `/var/folders`, a `.cache` folder) so new sessions there are neither captured nor queued. It is a separate button and previews like any edit.

### Alternatives

- A glob box in the editor that ticks matching rules, with bulk Remove / Remove and hide / Merge. Not chosen: grouping by the doctor's own codes needs no pattern from the operator, and the merge tool already covers ticked rules.
- Adding the deny rule as part of the bulk remove. Not chosen: a deny rule refuses sessions on every device that takes the profile, which is a wider decision than tidying stale allow rules.

## Acceptance criteria

- [x] Two or more findings of one code show as one Health row with a count, the shared message, and the rules in an expandable list
- [x] The group's bulk button applies every remove, narrow and remove-and-hide fix of that code in one preview, within the one-save hide limit
- [x] The editor offers a deny rule for the temporary roots of removed or flagged temporary folders, not covered by an existing deny, and applies it only when pressed
- [x] Tests cover grouping, the bulk fix and the deny offer; docs describe them

## Implementation plan

profiledoctor gains ApplyFixes (apply many remove/narrow/safe findings from one report, highest index first, on a copy) and TemporaryDeny (temp root or .cache folder for a temporary dir). web: healthView.Groups groups items by code in first-seen order; a group of 2+ shows the shared message once, the projects its rules admit, a details list of the per-rule findings (their own buttons kept), and in the editor a fix_all=CODE button labelled from bulkLabel, except when every fix is safe (Apply safe fixes covers that). tidyForm handles fix_all (refused past maxReviewBatch hides, like narrow) and deny_temporary (adds temporaryDenies: roots of flagged temporary rules and of temporary cwd keys queued to hide, less roots an existing deny covers). renderEditor sets the offer via offerDeny, so it survives the bulk fix through the queued hides, and shows outside the findings list so it remains when no finding does.

## Summary

Merged in #247. The Health panel groups findings of one code into one row with the shared message, the distinct projects the rules admit, and the per-rule findings under Show the N findings. In the editor a group gets one fix_all=CODE button (profiledoctor.ApplyFixes), except a group of only safe fixes; it is refused past one save's hide limit. The editor offers deny rules for the temporary roots (profiledoctor.TemporaryDeny) of flagged or just-removed temporary folders, never applied unasked. terva-review found the group count summed overlapping admits; fixed with a test.
