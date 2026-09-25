---
schema: 3
id: TKT-01M3B369M8X0S600AH04T5DBXM
title: Close the landed normalize epic and Cursor IDE ticket
type: chore
status: ready
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
updated_at: 2026-09-25T01:34:32Z
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

- [ ] Both tickets are done, with any criterion that does not hold noted
