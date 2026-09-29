---
schema: 3
id: TKT-01M3NM01Q9PCPWKTW0G2AP2QYT
title: "Allow: choose repository or owner width for the new rule"
type: task
status: ready
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

- [ ] Device Allow and batch Allow offer repository or owner width for a project with a remote; repository is the default
- [ ] Owner width is not offered when the owner path is the bare host
- [ ] A batch makes one prefix rule per owner and removes allow rules the new rules cover, naming the count
- [ ] The review API takes an optional width and existing callers are unchanged; docs updated
