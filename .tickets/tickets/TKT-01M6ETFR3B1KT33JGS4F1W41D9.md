---
schema: 3
id: TKT-01M6ETFR3B1KT33JGS4F1W41D9
title: "Review queue: group projects by owner or folder, hide temporary ones"
type: task
status: in-progress
status_reason: null
priority: high
due_on: null
labels:
  - area/server
  - policy
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim:
  actor: agent:claude-code/t3-43103b81
  branch: t3/review-groups
  worktree: /home/sothr/.t3/worktrees/lampi/t3-43103b81
  commit: 2f03aa5124cd3c8f030f0c1b08c84b5f458cba3d
  session: null
  claimed_at: 2026-11-02T19:06:37Z
  expires_at: null
archive: null
created_at: 2026-11-02T19:06:27Z
updated_at: 2026-11-02T19:23:40Z
created_by:
  id: agent:claude-code/t3-43103b81
  name: ""
updated_by:
  id: agent:claude-code/t3-43103b81
  name: ""
extensions: {}
---

## Description

The review queue on the live lake holds 536 projects, and 527 of them are folders under one parent (`~/.local/state/vuoro/worktrees`). The page lists each on its own row, so the operator pages through hundreds of rows that one rule or one hide would settle. The profile doctor already groups the queue by repository owner or parent folder (`review_group`), but only the Health panel shows it.

The owner chose (2026-11-02): a grouped queue on the review page, temporary folders as the only hide hint, dashboard only (no CLI).

### What changes

- Needs review groups the projects that share a repository owner or a parent folder, three or more to a group, the same grouping the doctor uses, into one collapsible row each. A group offers Allow OWNER/… (one rule per profile, through the existing confirm page) and Hide all, and expands to its projects with their usual row actions.
- Projects in temporary folders (the doctor's `Temporary` test) form their own group, which offers Hide all and no Allow. A lone temporary project keeps its row with a hint.
- Group actions name the group, not its keys, so a group larger than the 500-project batch limit works. The server expands the group from the queue as it reads it now, under the page's filter.
- GET /api/web/v1/review gains a `groups` list with each group's kind, rule and keys. needs_review keeps every project.

## Acceptance criteria

- [x] Needs review collapses projects sharing an owner or parent folder (3 or more) into one group row with Allow OWNER/… and Hide all
- [x] Temporary-folder projects group under a hide-only group
- [x] Group actions work past the 500-project batch limit and expand under the page's filter
- [x] GET /api/web/v1/review lists the groups; tests and docs cover the change

## Implementation plan

1. profiledoctor: export `GroupOf(remote, cwd)`, the owner prefix or parent folder the doctor's `pending` groups by, and use it there.
2. web/review_groups.go: after readReview builds Needs, `groupNeeds` sorts each row into a temporary group (every copy in a temporary folder), an owner or folder group, or no group. Groups under MinFold dissolve back into single rows. reviewView gains Groups (JSON `groups`: kind, id, rule, count and keys) and Singles (page only). A single temporary row is marked for a hint.
3. Forms: `group=ID` stands in for `key` on /review/allow, /review/allow/save and /review/hide. `groupKeys` re-reads the queue under the return's filter and expands the group. Allow forces owner width and refuses a temporary group. Hide from a group may pass maxReviewBatch, bounded by maxGroupKeys. The confirm page carries the group instead of its keys.
4. Template: Needs review shows the groups first, each a <details> with a summary row (count, sessions, size, newest, the rule) and the actions, then the single rows in the existing table.
5. Tests: grouping (owner, folder, temporary, under MinFold, wide folders), the group allow and hide past 500, a stale group, the API groups. Docs: web-dashboard.md and web-api.md.

Rejected alternatives: posting the expanded keys from the page breaks at 500 keys and sends a megabyte form; raising maxReviewBatch everywhere loosens the API for one case; a separate triage page was the owner's second choice.

## Notes

**agent:claude-code/t3-43103b81** at 2026-11-02T19:14:04Z

Implemented on t3/review-groups. Groups come from profiledoctor.GroupOf, which the doctor's review_group finding and the Allow buttons' owner width now share, so a group's rule is the rule each row's Allow FOLDER/… would add. Groups sort by size, so the temporary group can come first. Select-all now ticks only its own table: before, one header box on a page of group tables would tick every group and overrun the 500 limit. GOFLAGS=-mod=mod just ci passes.

**agent:claude-code/t3-43103b81** at 2026-11-02T19:23:40Z

terva-review on #241 (run 2253): a group named only by kind and owner/folder let Hide all reach projects that joined the group after the page loaded. Fixed: each group carries a version, sha256 of its keys cut to 16 hex digits. Every group form sends it as group_version, and groupFrom refuses a mismatch, so a changed or unversioned group answers 409 and changes nothing. Test: TestReviewGroupRefusesChangedMembers.
