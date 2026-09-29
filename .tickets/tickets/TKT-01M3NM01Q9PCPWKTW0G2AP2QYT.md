---
schema: 3
id: TKT-01M3NM01Q9PCPWKTW0G2AP2QYT
title: "Allow: choose repository or owner width for the new rule"
type: task
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
assignees: []
milestone: null
parent: TKT-01M3NM01J4731BSWEA815FWGXS
origin: null
dependencies:
  - TKT-01M3NM01KV7WF9SE218PWHR2GN
  - TKT-01M3NM01NKV3Q4K0TKYRQFTE77
blocks_on: none
references: []
claim:
  actor: agent:claude-code/58fb7d84
  branch: profile-rules/allow-width
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-58fb7d84
  commit: 057c35bbf0830d5b7bf340059e8981bf9382af4e
  session: null
  claimed_at: 2026-09-29T03:53:32Z
  expires_at: null
archive: null
created_at: 2026-09-29T03:40:36Z
updated_at: 2026-09-29T04:00:55Z
created_by:
  id: agent:claude-code/58fb7d84
  name: ""
updated_by:
  id: agent:claude-code/58fb7d84
  name: ""
extensions: {}
---

## Description

Allow on a device page, and Allow selected on the review page, always add the exact rule: the folded `git_remote`, or the `cwd_prefix` when there is no remote (`allowRule`, `internal/web/device_allow.go`). This is how the default profile reached about 97 rules.

### Approach

Offer a width when allowing a project that has a git remote:

- **This repository:** `git_remote`. This stays the default.
- **Its owner:** `git_remote_prefix` of the folded remote minus its last path segment, such as `git.example.com/team` for `git.example.com/team/app`. Do not offer it when the owner path would be the bare host, since a host-wide rule belongs in the editor where it is typed on purpose.

A project with no remote gets `cwd_prefix`, as today.

In a batch, projects that pick owner width under the same owner produce one rule, not one each. Before adding a rule, drop the existing allow rules it covers, using `config.Covers` from TKT-01M3NM01N (Profile editor: find covered rules and fold owner groups), and say in the notice how many were removed. The editor, or the batch confirm page, previews what the wider rule admits, using TKT-01M3NM01K (Profile preview: list the projects a change admits and drops).

The review API (`POST /api/web/v1/review/allow`) takes an optional width per key or per request, with `repository` as the default, so existing callers do not change.

### Files

- `internal/web/device_allow.go` and `internal/web/review_allow.go`
- `internal/web/templates/page.html`
- `docs/web-dashboard.md` and `docs/web-api.md`

## Acceptance criteria

- [x] Device Allow and batch Allow offer repository or owner width for a project with a remote; repository is the default
- [x] Owner width is not offered when the owner path is the bare host
- [x] A batch makes one prefix rule per owner and removes allow rules the new rules cover, naming the count
- [x] The review API takes an optional width and existing callers are unchanged; docs updated

## Implementation plan

- `internal/web/profile_tidy.go` holds the shared pieces:
  - `readWidth` reads `repository` or `owner`;
  - `ownerOf` names the owner, or nothing when it would be the bare host;
  - `allowRuleAt` builds the rule at a width;
  - `withRules` adds rules and drops the existing ones they cover.
- `deviceAllowPage` and `planAllow` take the width. `planAllow` merges with `withRules`, and each planned profile carries `Removed` and `Reach`, the admitted list from TKT-01M3NM01K.
- The UI:
  - the device page and each review row get an **Allow OWNER/…** button beside Allow;
  - both Allow selected forms get a **Rules for** select;
  - the confirm page lists the dropped rules and the admitted projects, and carries the width into its save form.
- The API takes `width`.

## Notes

**agent:claude-code/58fb7d84** at 2026-09-29T04:00:54Z

### Decisions

- **One width per request, not per project.** A select beside Allow selected, and one extra button on single Allow, cover the need. Per-row selects on a 500-row batch would be noise, and a project that should stay exact can be allowed on its own.
- **Owner width falls back to the exact rule.** It does so for a bare-host owner and for a folder, rather than refusing, so a mixed selection still makes a plan. The confirm page shows each rule, so the fallback is visible.
- **`withRules` removes only rules the new rules strictly cover.** When an existing rule equals a new one, the existing rule stays and the new one is not added. A new rule that an existing rule covers is not added either. Among the new rules, a wider owner covers a nested group.
- **Several projects can share a rule at owner width.** The API keeps `key` on a rule as the first selected project it is for, and does not add a list of keys. The confirm page already names the devices, and the admitted list names every project.
- **`profileReach` and `projectChange` gained JSON tags,** so the API returns `reach`.

### Verification

- `GOFLAGS=-mod=mod just ci` passes.
- New tests: `TestAllowSelectedAtOwnerWidth`, `TestAllowSelectedAPIWidth`, `TestAllowOneProjectAtOwnerWidth` and `TestWithRules`.
- The "drops N rules" wording was wrong in the first draft. It counted the dropped rules where it meant the new ones. The test caught it.
