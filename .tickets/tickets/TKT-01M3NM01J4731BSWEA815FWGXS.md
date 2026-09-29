---
schema: 3
id: TKT-01M3NM01J4731BSWEA815FWGXS
title: "Concise profile rules: wider matches and fewer rules"
type: epic
status: ready
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
claim: null
archive: null
created_at: 2026-09-29T03:40:36Z
updated_at: 2026-09-29T03:41:08Z
created_by:
  id: agent:claude-code/58fb7d84
  name: ""
updated_by:
  id: agent:claude-code/58fb7d84
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
