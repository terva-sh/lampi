---
schema: 3
id: TKT-01M3B369M8X0S600AH04T5DBXM
title: Close the landed normalize epic and Cursor IDE ticket
type: chore
status: done
status_reason: null
priority: low
due_on: null
labels:
  - area/normalize
assignees: []
milestone: null
parent: TKT-01M3B35JS8J2F83FG4J7ZC0199
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-25T01:34:31Z
updated_at: 2026-09-25T02:20:19Z
created_by:
  id: agent:claude-code/eh1m
  name: Claude Code cloud agent
updated_by:
  id: agent:claude-code/eh1m
  name: Claude Code cloud agent
extensions: {}
---

## Description

Two tickets are still `ready` although the work landed.

TKT-01M38RJTB89GY6GWC3XZZFYYY9 (Cursor IDE normalize projector) landed as PR #39 (6d7b567). Its parent, TKT-01M38RJCDREDTTPTY2D7SR8W59 (Normalize projectors for non-terva harnesses), has every child landed: Cursor CLI was PR #41 (f75d678). `docs/architecture.md` already names all six projectors, which was the epic's documentation criterion.

Check each acceptance criterion against the code, tick what holds, note anything that does not, and close both.

## Acceptance criteria

- [x] Both tickets are done, with any criterion that does not hold noted

## Implementation plan

Ticket store only; no code changes.

1. Verify each acceptance criterion and DoD item of TKT-01M38RJTB89GY6GWC3XZZFYYY9 (Cursor IDE normalize projector) against main: internal/normalize/cursor.go, internal/api/project.go, their tests, internal/cli/export_cursor_test.go, and the adapter history.
2. Verify the epic TKT-01M38RJCDREDTTPTY2D7SR8W59 (Normalize projectors for non-terva harnesses): child statuses, worker tests per harness, docs/architecture.md wording.
3. Run the relevant tests and the CI-equivalent go checks.
4. Tick what holds, note the evidence and any drift, set both done, then close this chore. Run check --strict and doctor.

## Notes

**agent:claude-code/eh1m** at 2026-09-25T02:20:14Z

Every criterion on TKT-01M38RJTB89GY6GWC3XZZFYYY9 (Cursor IDE normalize projector) and on the epic TKT-01M38RJCDREDTTPTY2D7SR8W59 (Normalize projectors for non-terva harnesses) holds on main at 2a0726c. Both are ticked and done, with the evidence in a note on each. Nothing failed. The Cursor IDE description predates PR #41 (cursor-cli dispatch) and PR #53 (reader version 2); its note records that drift. No code changed.

## Summary

Closed TKT-01M38RJTB89GY6GWC3XZZFYYY9 and TKT-01M38RJCDREDTTPTY2D7SR8W59 after checking each criterion against main. All held. Ticket store only.
