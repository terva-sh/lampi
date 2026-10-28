---
schema: 3
id: TKT-01M61MZMXHF57GKYYRE9ZQ4S3S
title: "Normalize: tool error flag for OpenCode, Cursor and grokbot"
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/normalize
assignees: []
milestone: null
parent: TKT-01M61MYZ6H6J8MGNPWHS8MTJ1X
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-28T16:20:09Z
updated_at: 2026-10-28T16:20:09Z
created_by:
  id: agent:claude-code/049834bc
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/049834bc
  name: Claude Code local agent
extensions: {}
---

## Description

`tool.is_error` is set by the Claude, Codex, Grok and terva normalizers only. OpenCode, Cursor IDE, Cursor CLI and grokbot leave it null on every result, so tool error rates and error-streak findings are blank for those harnesses.

For each of them, find whether the source records a tool failure (a status field, an error part, an exit code) and map it to `is_error`. Where the source records nothing, leave it null and say so in docs/harnesses.md, so null keeps meaning "not known", not "succeeded".

## Acceptance criteria

- [ ] Each of OpenCode, Cursor IDE, Cursor CLI and grokbot sets is_error where its source records a failure.
- [ ] Where a source records nothing, is_error stays null and docs/harnesses.md says so.
