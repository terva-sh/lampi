---
schema: 3
id: TKT-01M6EJ43HT4EZH4Y51221C3CGE
title: "Profile doctor: rule checks and serve profiles doctor"
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
dependencies:
  - TKT-01M6EHCSYRP3BACSX2N16H58PX
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-11-02T16:40:17Z
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

The live `default` profile reached the 500-rule cap on 2026-11-02 with about 234 allow rules and 483 projects waiting in review. Many rules were wrong in ways the editor did not flag:

- `cwd_prefix /home/steamos` is a home directory, the hazard in TKT-01M3NQ8324 (Allow on a folder project allows everything under it, home dirs too).
- `cwd_prefix /tmp` is a top-level folder. About 30 rules sit under it, and others name `/var/folders/.../T/Test.../001` and `tmp.XXXX` folders that no new session will use.
- About 32 rules are covered by other rules.

Add `internal/profiledoctor`. It reads a profile's rules against the newest inventory of each device that takes them, and reports findings with a level, a code and a fix. Add `terva-lampi serve profiles doctor NAME [--json] [--write FILE]`, which prints the report. With `--write`, it writes the profile with the safe fixes applied, ready for `serve profiles set`.

### Decisions (operator, 2026-11-02)

- **Surfaces:** the dashboard and the lake CLI. No web API and no check of the agent's local rules for now.
- **Fixing:** the safe fixes are applied in one step, and the narrowing and widening fixes one at a time with a preview. The doctor itself never saves.
- **Temporary rules still in use:** remove the rule and hide what it admitted from review in the same step.

### Design

- **Safe means provably no verdict change:** covered, shadowed by a deny rule, or a temporary folder that no listed device holds. A rule that is merely unmatched is not safe, because it may be for a repository nobody has opened yet. It gets its own fix.
- **A temporary rule is safe only when every device sent an inventory.**
- **Narrowing a home, top-level or host-wide rule** replaces it with exact rules for the projects it admits today. A project in the folder itself gets a `cwd_hash`, which names that folder alone.
- **The existing tidy helpers move into the package** (coverage, owner and folder folds, the rule cap, RuleText), so the CLI and the web share one copy.
- **Deny matching** reads the profile the way the agent does, so a deny `git_remote_prefix` also refuses folder projects whose remote is unknown. The preview's reach does the same.

### Alternatives considered

- **Removing rules that have gone quiet, by newest session time.** Rejected: removing them puts their projects back in review whenever the device still holds the sessions.
- **One proposed full document with every fix.** The operator chose one-step safe fixes instead, since widening is an operator decision (TKT-01M3NM01J).
- **Calling `internal/web` from the CLI.** Rejected: the web package carries HTTP state, and the doctor is pure arithmetic with one gather function over the catalog.

## Acceptance criteria

- [x] internal/profiledoctor reports home, broad, host-wide, covered, shadowed, temporary, unmatched, fold, cap and review-group findings
- [x] Safe fixes change no project's verdict on a listed device, and a missing inventory makes a temporary rule unsafe
- [x] serve profiles doctor prints the report, --json prints it as JSON, and --write writes the profile with the safe fixes applied
- [x] docs/vps-bringup.md documents the command and every code

## Notes

**agent:claude-code/t3-43103b81** at 2026-11-02T16:45:47Z

Committed on t3/profile-doctor (ff0aa75), stacked on t3/profile-cleanup-tooling (066b521, TKT-01M6EHCSYRP3BACSX2N16H58PX). GOFLAGS=-mod=mod just ci passes. Not pushed: waiting on the operator. Not run against the live lake: /var/lib/terva-lampi/catalog.db is not readable as the workstation user, so the first real report comes from the operator running serve profiles doctor default as the lake's user.

## Summary

Merged as #234 (7e8d547). internal/profiledoctor reports home, broad, host-wide, covered, shadowed, temporary, unmatched, fold, review-group, cap and unlisted-device findings against the devices' newest inventories. serve profiles doctor prints them, with --json and --write for the safe fixes. terva-review found two bugs, both fixed before merge: a narrowing could replace a folder rule with a bare git_remote, which admits checkouts outside the folder, so each replacement now stays within the rule it replaces; and Remove and hide built its list from the 50 projects shown. Ships in v0.11.0.
