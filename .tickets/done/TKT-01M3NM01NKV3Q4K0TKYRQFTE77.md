---
schema: 3
id: TKT-01M3NM01NKV3Q4K0TKYRQFTE77
title: "Profile editor: find covered rules and fold owner groups"
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
parent: TKT-01M3NM01J4731BSWEA815FWGXS
origin: null
dependencies:
  - TKT-01M3NM01KV7WF9SE218PWHR2GN
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-29T03:40:36Z
updated_at: 2026-09-29T04:54:29Z
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

- [x] config.Covers decides allow-rule coverage field by field, with table tests including the false-in-doubt cases
- [x] The preview marks each covered allow rule, names its covering rule, and offers one action to remove them
- [x] Three or more exact git_remote rules under one owner produce a suggestion that rewrites the form to one git_remote_prefix
- [x] Neither action saves; docs describe both

## Implementation plan

- `config.Covers` in `internal/config/policy.go`: allow-rule coverage, decided field by field.
- `internal/web/profile_tidy.go`:
  - `coveredBy`, `coveredRules` and `withoutCovered` handle the covered rules.
  - `ownerFolds` and `foldOwner` handle the owner folds.
  - `tidyForm` applies the button the operator pressed to the form before the preview.
- The editor view carries `Covered` and `Folds`, rendered as a "Fewer rules" panel inside the editor form.
- `ruleText` names every field a rule sets.

## Notes

**agent:claude-code/58fb7d84** at 2026-09-29T03:53:19Z

### Decisions

- **Covers is conservative.** A field of the wider rule must follow from a field of the narrower one:
  - a `cwd_prefix` from a `cwd_prefix` at or under it;
  - a `git_remote` from the same folded remote;
  - a `git_remote_prefix` from a remote or a prefix under it;
  - a `cwd_hash` from the same hash.

  Nothing is inferred across kinds, for example a hash from a prefix. `TestCoversIsSound` checks on a grid of rules and IDs that `Covers(a,b)` and "b matches" imply "a matches".
- **Deny rules are not offered.** A deny rule reads a doubt as a match, so coverage between deny rules needs its own definition. A redundant deny rule is also harmless.
- **Duplicates.** When two rules cover each other, the first one stays. Covers is transitive, so removing every covered rule keeps some rule that nothing covers, and loses nothing.
- **Fold scope.**
  - Only rules that set nothing but `git_remote` count. A rule with an extra field is narrower on purpose, and folding it would widen more than it shows.
  - The owner is the folded remote less its last segment. Nested groups therefore fold to the group, not the top owner.
  - The threshold is 3.
  - A bare host is never offered, because a host-wide rule should be typed on purpose.
  - An owner whose prefix an existing rule already covers is not offered, since the covered-rule offer handles it.
- **Button placement.** The offers sit in the editor form after "Preview changes". Pressing Enter in a field submits the form's first submit button, which must stay the plain preview. The test checks the order.
- **Notes.** An offer fills an empty note with what it did, so the revision says why the rules went. A note the operator typed is kept.
- **Form input.** `fold=OWNER` comes from the form, but it only acts on remotes the form already holds. An owner the form holds no remote under changes nothing.

### Verification

`GOFLAGS=-mod=mod just ci` passes. The tests are `TestCovers`, `TestCoversIsSound`, `TestEditorOffersFewerRules` and `TestOwnerFolds`.

**agent:claude-code/58fb7d84** at 2026-09-29T04:42:48Z

Review 1333 on PR 140, finding 1 (medium): the Fewer rules hint promised that the preview lists every project a change admits. It lists only what the devices' newest inventories show. Accepted, and the hint was reworded in 80fe1c39be472cd95771054b143abfb06a7fd278.

**agent:claude-code/58fb7d84** at 2026-09-29T04:44:43Z

Review 1335 on PR 140, finding 1 (medium): tidyForm folded any owner a form named, which let a crafted form fold fewer remotes than an offer needs. Accepted, and fixed in c4b8dd715ea10c72f0b12dde1bfb407b71bd4c68: it now folds only an owner that ownerFolds offers for the submitted rules. TestEditorOffersFewerRules covers an owner with two remotes.

## Summary

Landed in #140 (merge 19ab892). config.Covers decides allow-rule coverage field by field, conservatively. TestCoversIsSound checks it on a grid of rules and IDs. The editor's Fewer rules panel removes covered allow rules and folds three or more exact remotes under one owner into one git_remote_prefix, only for an owner it offers. Both change the form and preview it; nothing is saved without the operator. Reviews 1333 (the hint overclaimed the preview) and 1335 (a fold accepted any owner) were fixed before merging.
