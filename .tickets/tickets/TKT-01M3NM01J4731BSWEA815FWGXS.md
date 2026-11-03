---
schema: 3
id: TKT-01M3NM01J4731BSWEA815FWGXS
title: "Concise profile rules: wider matches and fewer rules"
type: epic
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - policy
  - area/server
  - area/agent
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim:
  actor: agent:codex/t3-6387cc8a
  branch: feat/profile-hide-prefix
  worktree: /home/sothr/.t3/worktrees/lampi/t3-6387cc8a
  commit: 20e91767d34ae7037d2f8011b0c613254429ca39
  session: null
  claimed_at: 2026-11-03T17:26:49Z
  expires_at: null
archive: null
created_at: 2026-09-29T03:40:36Z
updated_at: 2026-11-03T17:36:51Z
created_by:
  id: agent:claude-code/58fb7d84
  name: ""
updated_by:
  id: agent:codex/t3-6387cc8a
  name: ""
extensions: {}
---

## Description

The internal lake's `default` profile has grown to about 97 allow rules. The rules are hard to review, and each new repository needs another round of review before it uploads.

The cause is in how rules are written, not in what the profiles need to say:

- **Allow writes one exact rule per project.** `allowRule` in `internal/web/device_allow.go` adds the project's folded `git_remote`, or its `cwd_prefix` when it has no remote. The batch Allow on the review page uses the same function. Nothing offers the wider `git_remote_prefix` that TKT-01M3HKYFE added, and nothing notices that a new rule makes older ones redundant.
- **Nothing shows what a wider rule admits.** The editor preview lists the devices a profile reaches and the line diff. It does not list which of their inventoried projects the change newly admits or drops, so widening a rule is a guess.
- **`cwd_prefix` names a path on one machine.** A directory with no git remote (worktrees, scratch trees, notes) needs one rule per machine and per home directory.

### Goal

An operator can describe the sessions they care about in a handful of rules, and can see exactly which projects a rule change admits before saving it.

### Tickets, in order

1. The preview lists the projects a change admits and drops. The other tickets rely on it to make widening safe.
2. The editor finds rules that another rule already covers, and offers to replace a group of exact remotes under one owner with a prefix.
3. Allow offers a width: this repository, or everything under its owner.
4. A `cwd_glob` rule field, so one rule covers a directory layout on every machine.
5. Consolidate the internal lake's `default` profile with the tools above.

### Decisions

- **A new field, not glob syntax in an existing one.** TKT-01M3HKYFE rejected glob syntax inside `git_remote` because it changes what existing rules mean and makes exact matches ambiguous. `cwd_glob` is a new field for the same reason.
- **No `git_remote_glob` for now.** `git_remote_prefix` already covers an owner, a group or a host. The case left over, one repository name under any owner (forks), has no user yet. File it when one appears.
- **No named rule sets or rule sharing between profiles.** Several profiles hold the same list only when several profiles exist. The problem today is one profile that is too long.
- **Widening stays an operator decision.** The tools suggest and preview. Nothing widens a profile without a save the operator makes.

## Implementation plan

Implement the owner-requested prefix hide increment: consolidate selected temporary allow rules into a profile-local review hide prefix, with narrower show rules or structurally narrower allow rules as exceptions. Keep deny-wins capture matching unchanged. Store review policy in profile revisions for atomic saves and rollback, but strip it from resolved device profiles so older agents receive the same capture schema and versions. Match directory boundaries using inventoried paths, never lake-host filesystem resolution. Preserve explicit per-project hides and mixed temporary/non-temporary copies. Add editor rules, merge-to-hide preview including dropped capture reach and visibility changes, API/CLI document support, and docs. Validate boundary/tie/exception behavior, future inventories, filtered queues, round trips, stale saves, rollback and device profile compatibility. No live profile or capture changes are authorized by this implementation. The existing live consolidation child remains independent.

## Notes

**agent:claude-code/58fb7d84** at 2026-09-29T06:17:57Z

### Status after the first run, 2026-09-29

Four of five tickets have landed on `main`, each through its own PR with CI and terva-review:

- TKT-01M3NM01K (Profile preview: list the projects a change admits and drops), #131.
- TKT-01M3NM01N (Profile editor: find covered rules and fold owner groups), #140.
- TKT-01M3NM01Q (Allow: choose repository or owner width for the new rule), #141.
- TKT-01M3NM01S (cwd_glob rule field for allow and deny), #145.

TKT-01M3NM01T (Consolidate the internal lake's default profile) is blocked on two things. It needs an operator dashboard session, which an agent cannot get through OIDC, and a lake release that carries the four PRs. The live profile was never read during this run.

### How it went

- **Build order.** Each ticket depends on the one before: the preview, then `Covers`, then the Allow width, then the glob. So the branches were stacked, and each was brought up to date by merging `main` after its predecessor landed. The branches were never rebased once pushed, so no force-push was needed.
- **Reviews.** terva-review found real problems in every PR, and each was fixed before merging. The per-ticket notes record them. The biggest was the preview's local-deny inference, which took two rounds to get right (reviews 1318 and 1320).
- **Merging under a moving `main`.** Other agents merged about every 15 minutes. Every merge was preceded by a full `just ci` on a scratch worktree of the PR merged with the newest `origin/main`.
- **One mistake.** #141 merged while its last Lint and Test run was red. The failure was the known `internal/cli` load flake, TKT-01M3MJDS. The merge step checked that the branch held `main` but not the CI result. `main`'s own CI on the merge commit passed. From #145 on, the merge step checks every status.
- **Filed on the way.** TKT-01M3NW0VQW (Flaky under load: hangup reload outlives its test and panics). It failed #145's first CI run and is unrelated to this epic.

### Not done, by decision

- `git_remote_glob`: no user yet.
- Coverage between deny rules: a redundant deny rule is harmless.
- Glob-contains-glob in `Covers`: only identical patterns count.

**agent:codex/t3-6387cc8a** at 2026-11-03T17:36:51Z

### Prefix hiding increment

Implemented the owner's requested alternative to the refused broad `/tmp` allow merge. Profiles now retain `review.rules` with directory Hide/Show actions. The deepest matching prefix wins, Hide wins a tie, and a matching structurally narrower capture allow (or exact cwd_hash) is an exception. Conversion removes only selected covered folder allow rules, preserving unticked exceptions and existing Show rules. Preview separates stopped uploads from review visibility changes; saving and rollback treat both as one profile document.

Kept capture deny-wins semantics rather than overloading deny: making narrower allows override denies would change the upload privacy boundary. Review policy stays on the lake and is stripped from signed device documents before versioning, keeping existing agents compatible. Manual per-project hides remain authoritative and lake-wide. Prefix matching uses inventoried path boundaries without resolving paths on the lake host. Mixed-checkout inventories remain visible because one representative path cannot prove all copies match. Hiding one sighting never hides another profile/device's visible copy.

Automatic allow insertion and doctor cleanup preserve review exceptions, including otherwise capture-redundant rules. Folder folds involving exceptions are not offered. The doctor excludes prefix-hidden sightings from pending suggestions. Review-only saves can conservatively keep an already-allowed refusal pending until another inventory arrives, since the existing caught-up check uses the latest save time; no permission inference was weakened.

Regression coverage exercises the 21-rule `/tmp` scenario via actual rendered editor forms, preview without mutation, later inventories, directory boundaries, Show/deeper-Hide exceptions, separate profile/device sightings, mixed checkouts, manual hides, stale saves, rollback, protected cleanup, and signed device schema/version stability. Documented editor usage and CLI/API JSON. Focused regression tests and vet pass; full suite, affected race suites and build are being completed before PR submission. No live profile, capture setup or service was changed. The existing live consolidation child remains independent.
