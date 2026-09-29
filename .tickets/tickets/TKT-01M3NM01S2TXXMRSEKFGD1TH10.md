---
schema: 3
id: TKT-01M3NM01S2TXXMRSEKFGD1TH10
title: cwd_glob rule field for allow and deny
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/agent
  - policy
assignees: []
milestone: null
parent: TKT-01M3NM01J4731BSWEA815FWGXS
origin: null
dependencies:
  - TKT-01M3NM01NKV3Q4K0TKYRQFTE77
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-29T03:40:37Z
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

`cwd_prefix` names a path on one machine. A directory with no git remote needs one rule per machine and per home directory, such as the worktrees a tool creates under `~/.t3/worktrees/<project>/<id>`, or notes and scratch trees. A pattern field would cover one layout everywhere.

### The field

Add `cwd_glob` to `ProjectMatch`. It sits beside the other fields and is ANDed with them like any other field.

- **Syntax.** Segments are separated by `/`. A `*` inside a segment matches any run of characters except `/`. A segment that is exactly `**` matches zero or more whole segments. Every other character is literal, so there are no `?`, brackets or escapes. That keeps a path containing `[` or `?` literal, as it is in `cwd_prefix`.
- **Prefix semantics.** The rule matches when the pattern matches the cwd or any ancestor of it, as `cwd_prefix` matches on a boundary. `/home/*/notes` then covers `/home/me/notes/2026`. A trailing `/**` adds nothing.
- **Validation.** Enforced where a profile and `config.json` are checked. The pattern is absolute, starts with `/`, and has no empty, `.` or `..` segment. It holds at least one segment with no `*`, so `/**` and `/*/**` are refused rather than allowing the world. The empty-rule rule already covers the empty string. A drive-letter path is out of scope until a Windows agent reports one.
- **Allow rules** compare exactly, with case kept and the cwd as recorded.
- **Deny rules** read a doubt as a match, as `cwd_prefix` does. They fold case, and they test the recorded cwd and its symlink-resolved form. The pattern itself is not resolved, because a pattern cannot be.

### Knock-on changes

- `ProjectMatch.empty`, `normalizeRules` (trim) and `readRules`. The editor's rule table gets a `cwd_glob` column, and `ruleText` handles the field.
- `config.Covers`, from TKT-01M3NM01N (Profile editor: find covered rules and fold owner groups), answers false for any pair involving a glob, unless the glob strings are equal.
- `docs/allowlist-and-redaction.md`, `docs/policy.md` and `deploy/profiles.json.example`.

### Compatibility

Profiles decode strictly. An older lake refuses to load a `profiles.json` that uses the field, and an older agent keeps its cached profile and reports the error, which fails safe. Document the upgrade order: the lake first, then the agents. This is the same order as `git_remote_prefix`.

## Acceptance criteria

- [ ] cwd_glob matches with * inside a segment and ** across segments, with prefix semantics
- [ ] Validation refuses relative patterns, empty, . and .. segments, and patterns with no literal segment
- [ ] Deny cwd_glob folds case and tests the resolved cwd; allow compares exactly
- [ ] The editor reads and shows the field, and Covers is conservative for globs
- [ ] Docs and profiles.json.example describe the field and the upgrade order
