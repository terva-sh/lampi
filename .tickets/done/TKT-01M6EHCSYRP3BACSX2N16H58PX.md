---
schema: 3
id: TKT-01M6EHCSYRP3BACSX2N16H58PX
title: "Fold folder rules: Allow FOLDER/… and a cwd_prefix fold in the editor"
type: task
status: done
status_reason: null
priority: high
due_on: null
labels:
  - policy
  - area/server
assignees: []
milestone: null
parent: TKT-01M3NM01J4731BSWEA815FWGXS
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-11-02T16:27:33Z
updated_at: 2026-11-02T17:34:50Z
created_by:
  id: agent:claude-code/t3-43103b81
  name: ""
updated_by:
  id: agent:claude-code/t3-43103b81
  name: ""
extensions: {}
---

## Description

The live `default` profile reached the 500-rule cap. Allow selected on the review queue refused with "the allow rules would make an invalid profile: profile default: projects: at most 500 allow and 500 deny rules".

Two sources fill it with rules nobody needs one at a time:

- **Folders made per session.** 478 of 483 projects waiting for review on 2026-11-02 were `/home/sothr/.local/state/vuoro/worktrees/<id>`, each a folder project with no remote. Allow writes one `cwd_prefix` per project, so allowing them all would add 478 rules.
- **Sibling folder rules already in the profile.** About 60 rules are t3 worktrees under `/home/sothr/.t3/worktrees/<repo>/<id>`, kept as folders because the worktree was gone when the inventory named it. Others are macOS test temp dirs under one `/var/folders/.../T`, and folders under `/Users/drewshort/Workspace`.

The editor's Fewer rules offers (TKT-01M3NM01N) fold exact remotes under an owner, and nothing folds folders.

### Approach

- Allow at owner width gives a folder project one `cwd_prefix` for the folder above it, and the review and device pages get an **Allow FOLDER/…** button beside **Allow OWNER/…**.
- The editor offers to replace the `cwd_prefix`-only rules under a folder with one `cwd_prefix` for it, when they sit in at least three different subfolders and no rule covers the folder yet.
- Neither ever names a top-level folder, a home directory, or the folder holding home directories. That is the hazard in TKT-01M3NQ8324 (Allow on a folder project allows everything under it, home dirs too).

### Alternatives

- **A separate `parent` width.** Rejected: a mixed selection of repositories and folders would need four choices in the Rules for menu. Owner width already means one level up from the project, and the confirm page lists every rule and what it admits before a save.
- **`cwd_glob` folds**, such as `/home/*/.t3/worktrees/**`. Deferred: they match across machines, which is a wider decision than the operator made by allowing the folders. A cwd_prefix fold is easier to check in the preview.
- **Removing rules that have gone quiet.** Rejected for now: a removed rule puts its projects back in the review queue whenever the device still holds their sessions, which adds work instead of removing it.
- **Folding automatically on save.** Rejected by the epic's rule that widening stays an operator decision.

## Acceptance criteria

- [x] Allow at owner width adds one cwd_prefix for the folder above a folder project
- [x] The editor offers to fold cwd_prefix rules under a folder into one, and previews it
- [x] Neither offers a top-level folder, a home directory, or the folder holding home directories
- [x] docs/web-dashboard.md and docs/web-api.md describe both

## Implementation plan

Shipped in one PR on branch t3/profile-cleanup-tooling: parentOf/wideFolder/folderFolds/foldFolder in internal/web/profile_tidy.go; allowRuleAt uses parentOf at owner width; editor template gets a folder offer; review and device rows get Allow FOLDER/… buttons; tests in profile_tidy_test.go and allow_width_test.go; docs in web-dashboard.md and web-api.md.

## Notes

**agent:claude-code/t3-43103b81** at 2026-11-02T16:28:21Z

Dry run on 145 of the live default profile's cwd_prefix rules, 2026-11-02: Remove covered drops 32, mostly under /tmp, which the profile allows. Top folder offers: /home/sothr/.t3/worktrees (59 rules), /Users/drewshort/Workspace (13), /var/folders/b1/.../T (13), /home/sothr/.t3/worktrees/ledger (12), /home/sothr/workspace (11). Allow at owner width turns the 478 vuoro worktrees waiting for review into one cwd_prefix for /home/sothr/.local/state/vuoro/worktrees. GOFLAGS=-mod=mod just ci passes locally. Not pushed and no PR yet: waiting on the operator. The profile also holds cwd_prefix /home/steamos, a home directory: the TKT-01M3NQ8324 hazard, left for the operator.

## Summary

Merged as #232 (3993a92). Allow at owner width gives a folder project one cwd_prefix for the folder above it, through Allow FOLDER/… on the review and device pages and the batch Rules for option. The editor folds cwd_prefix rules spread over three or more subfolders of one folder. Neither names a top-level folder, a home directory, the folder holding home directories, or root's home (/root, /var/root); terva-review caught /var/root. Ships in v0.11.0.
