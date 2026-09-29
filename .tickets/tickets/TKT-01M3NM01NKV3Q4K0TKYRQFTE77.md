---
schema: 3
id: TKT-01M3NM01NKV3Q4K0TKYRQFTE77
title: "Profile editor: find covered rules and fold owner groups"
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
parent: TKT-01M3NM01J4731BSWEA815FWGXS
origin: null
dependencies:
  - TKT-01M3NM01KV7WF9SE218PWHR2GN
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

When a rule is widened by hand, the exact rules it now covers stay behind. Nothing points them out, so the list only grows. The editor should find allow rules that another allow rule already covers, and should offer to fold a run of exact remotes under one owner into a single `git_remote_prefix`.

### Covering

Add `config.Covers(a, b ProjectMatch) bool`: every project `b` matches, `a` also matches. It is decided field by field, is conservative, and answers false when in doubt. For a non-empty `a` and `b`, `a` covers `b` when every field set on `a` is implied by `b`:

- `a.CWDPrefix`: `b.CWDPrefix` is at or under it (`cwdHasPrefix`).
- `a.GitRemote`: `b.GitRemote` is the same remote after `NormalizeRemote`.
- `a.GitRemotePrefix`: `b.GitRemote` or `b.GitRemotePrefix` is at or under it (`remoteHasPrefix`).
- `a.CWDHash`: `b.CWDHash` is equal.

This definition is for allow rules, which compare exactly. Deny rules read a doubt as a match (case folding, symlinks, unknown remotes), and a redundant deny rule is harmless, so deny rules are out of scope.

### In the editor

- **Covered rules.** On the preview, mark each allow rule that another allow rule in the same document covers, name the covering rule, and offer one action that removes every covered rule. When two rules cover each other (duplicates), keep the first one.
- **Owner groups.** When three or more exact `git_remote` rules share an owner path (the folded remote minus its last segment), suggest replacing them with one `git_remote_prefix` for that owner. The suggestion rewrites the form only. The preview from TKT-01M3NM01K (Profile preview: list the projects a change admits and drops) then lists what the prefix would newly admit, and the operator saves or not.

Both actions change the form only. Nothing is saved without the operator.

### Files

- `internal/config/policy.go` (Covers, beside `matches`), with table tests
- `internal/web/profile_edit.go` and `internal/web/templates/page.html`
- `docs/web-dashboard.md`

## Acceptance criteria

- [ ] config.Covers decides allow-rule coverage field by field, with table tests including the false-in-doubt cases
- [ ] The preview marks each covered allow rule, names its covering rule, and offers one action to remove them
- [ ] Three or more exact git_remote rules under one owner produce a suggestion that rewrites the form to one git_remote_prefix
- [ ] Neither action saves; docs describe both
