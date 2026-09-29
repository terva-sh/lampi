---
schema: 3
id: TKT-01M3NM01S2TXXMRSEKFGD1TH10
title: cwd_glob rule field for allow and deny
type: task
status: done
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
updated_at: 2026-09-29T06:17:38Z
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

- [x] cwd_glob matches with * inside a segment and ** across segments, with prefix semantics
- [x] Validation refuses relative patterns, empty, . and .. segments, and patterns with no literal segment
- [x] Deny cwd_glob folds case and tests the resolved cwd; allow compares exactly
- [x] The editor reads and shows the field, and Covers is conservative for globs
- [x] Docs and profiles.json.example describe the field and the upgrade order

## Implementation plan

- `internal/config/glob.go`:
  - `checkGlob` validates a pattern.
  - `globHasPrefix` matches with prefix semantics, by dynamic programming over pattern and path segments.
  - `segmentMatch` handles `*` within one segment.
- `ProjectMatch.CWDGlob` (`cwd_glob`):
  - It is part of `empty()`.
  - `matches` compares exactly.
  - `denies` folds case, and runs once for the cwd as written and once resolved.
  - `Covers` treats it as implied only by the identical pattern.
- `Projects.Validate` is called from `Profile.Validate` and from `LoadFile`, for the top-level rules and each lake's rules.
- The web editor reads, trims and shows the field in every rule table and in the hidden form, and `ruleText` names it.
- Docs: `allowlist-and-redaction.md`, `policy.md`, and `deploy/profiles.json.example`.

## Notes

**agent:claude-code/58fb7d84** at 2026-09-29T04:05:12Z

### Decisions

- **Syntax is `*` and `**` only.** `?`, brackets and escapes would change the meaning of paths that hold those characters, which `cwd_prefix` takes literally. `path.Match` was rejected for the same reason, and because it has no `**`.
- **Prefix semantics, like `cwd_prefix`.** The pattern matches the cwd or any ancestor. A trailing `/**` therefore adds nothing, and `/home/*/.t3/worktrees` covers every worktree below it.
- **An invalid `config.json` pattern stops the agent.** `LoadFile` now validates rules, so a bad pattern is a load error, like a malformed file. The alternative, a deny pattern that silently matches nothing, would upload what the user meant to deny. As a second line, `denies` treats a pattern `checkGlob` refuses as a match, and `matches` treats it as no match.
- **Limits.** At most four `**` segments. The matcher is quadratic in segments, so the limit is about readability more than cost. A pattern needs at least one segment with no `*`, so `/**` and `/*/**` are refused.
- **The pattern itself is not symlink-resolved in a deny rule.** A pattern cannot be resolved. The cwd is tried both as written and resolved, which the existing `deniedByAny` loop already does.
- **`Covers` is conservative.** A glob is implied only by the identical glob. Checking whether one pattern contains another is out of scope.
- **Out of scope:** Windows drive-letter cwds, which never match a pattern starting with `/`. They are refused as they are today.

### Verification

- `GOFLAGS=-mod=mod just ci` passes.
- New tests: `TestGlobHasPrefix`, `TestCheckGlob`, `TestCWDGlobAllowAndDeny` (including a symlinked cwd), `TestCWDGlobIsValidatedWhereRulesLoad`, and `TestEditorSavesACWDGlob`.
- The `Covers` soundness grid now includes glob rules.

**agent:claude-code/58fb7d84** at 2026-09-29T06:01:11Z

Review 1355 on PR 145, finding 1 (medium): checkGlob refuses ** inside a longer folder name (proj-**), which the docs did not say. Accepted, and documented rather than changed, in bfe60a6296ff89543f00103267658035d408ba83. The refusal is deliberate: proj-** looks as if it reaches into subfolders, and it would not. The same CI run failed on TKT-01M3NW0VQW (Flaky under load: hangup reload outlives its test and panics), a serve test this PR does not touch.

## Summary

Landed in #145 (merge 7ee24f9). cwd_glob names a folder layout: * matches within one folder name, a ** folder matches any number of folders, and nothing else is special. It matches the folder and everything under it. Profiles and config.json refuse a pattern that is relative, has an empty, . or .. folder, has more than four **, or has no plain folder; an invalid pattern in config.json stops the agent. A deny glob ignores case and tries the resolved cwd. Review 1355 asked why ** inside a folder name is refused; that is now documented. Upgrade order: the lake first, then the agents.
