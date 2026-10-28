---
schema: 3
id: TKT-01M61MZMXHF57GKYYRE9ZQ4S3S
title: "Normalize: tool error flag for OpenCode, Cursor and grokbot"
type: task
status: in-progress
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
claim:
  actor: agent:claude-code/049834bc-norm
  branch: worktree-agent-ae1d2160e416e8e66
  worktree: /home/sothr/workspace/git.local.sothr.com/terva-sh/lampi/.claude/worktrees/agent-ae1d2160e416e8e66
  commit: 2439f8a0f31b8907b1905d5ac3ea5bd602ed46da
  session: null
  claimed_at: 2026-10-28T16:30:49Z
  expires_at: null
archive: null
created_at: 2026-10-28T16:20:09Z
updated_at: 2026-10-28T16:30:49Z
created_by:
  id: agent:claude-code/049834bc
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/049834bc-norm
  name: Claude Code local agent
extensions: {}
---

## Description

`tool.is_error` is set by the Claude, Codex, Grok and terva normalizers only. OpenCode, Cursor IDE, Cursor CLI and grokbot leave it null on every result, so tool error rates and error-streak findings are blank for those harnesses.

For each of them, find whether the source records a tool failure (a status field, an error part, an exit code) and map it to `is_error`. Where the source records nothing, leave it null and say so in docs/harnesses.md, so null keeps meaning "not known", not "succeeded".

## Acceptance criteria

- [ ] Each of OpenCode, Cursor IDE, Cursor CLI and grokbot sets is_error where its source records a failure.
- [ ] Where a source records nothing, is_error stays null and docs/harnesses.md says so.
